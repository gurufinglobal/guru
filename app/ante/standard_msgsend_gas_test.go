package ante

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	antetypes "github.com/cosmos/evm/ante/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptomultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func TestStandardMsgSendGasDecoratorEligibilityBoundaries(t *testing.T) {
	configureRouterEVM(t)

	dynamicExtension, err := codectypes.NewAnyWithValue(&antetypes.ExtensionOptionDynamicFeeTx{})
	require.NoError(t, err)
	unknownExtension, err := codectypes.NewAnyWithValue(&evmtypes.ExtensionOptionsEthereumTx{})
	require.NoError(t, err)

	codec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	other := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	otherAddress, err := codec.BytesToString(other)
	require.NoError(t, err)

	tests := []struct {
		name         string
		simulate     bool
		txBytes      []byte
		mutate       func(*standardMsgSendTestTx)
		wantStandard bool
		wantErr      bool
	}{
		{
			name:         "exact bounded MsgSend",
			wantStandard: true,
		},
		{
			name: "zero fee",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = nil
			},
			wantStandard: true,
		},
		{
			name: "one dynamic fee extension",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.extensions = []*codectypes.Any{dynamicExtension}
			},
			wantStandard: true,
		},
		{
			name:     "simulation may declare zero gas",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 0
			},
			wantStandard: true,
		},
		{
			name: "non-simulation zero gas",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = 0
			},
			wantErr: true,
		},
		{
			name:     "simulation still rejects below minimum gas",
			simulate: true,
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas - 1
			},
			wantStandard: false,
		},
		{
			name: "declared gas below exact value",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas - 1
			},
			wantErr: true,
		},
		{
			name: "declared gas above exact value",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.gas = StandardMsgSendGas + 1
			},
			wantStandard: true,
		},
		{
			name: "memo at limit",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.memo = strings.Repeat("m", StandardMsgSendMaxMemoBytes)
			},
			wantStandard: true,
		},
		{
			name: "memo above limit",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.memo = strings.Repeat("m", StandardMsgSendMaxMemoBytes+1)
			},
		},
		{
			name: "unordered transaction",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.unordered = true
			},
		},
		{
			name: "non-critical extension",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.nonCriticalExtensions = []*codectypes.Any{dynamicExtension}
			},
		},
		{
			name: "more than one critical extension",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.extensions = []*codectypes.Any{dynamicExtension, dynamicExtension}
			},
		},
		{
			name: "non-dynamic critical extension",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.extensions = []*codectypes.Any{unknownExtension}
			},
		},
		{
			name: "feegrant is excluded even when granter equals payer",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.granter = append(sdk.AccAddress(nil), tx.payer...)
			},
		},
		{
			name: "non-base fee denomination",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = sdk.NewCoins(sdk.NewInt64Coin("wrong-denom", 1))
			},
		},
		{
			name: "more than one fee denomination",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.fee = sdk.NewCoins(
					sdk.NewInt64Coin(evmtypes.GetEVMCoinDenom(), 1),
					sdk.NewInt64Coin("wrong-denom", 1),
				)
			},
		},
		{
			name: "no message",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs = nil
			},
		},
		{
			name: "more than one top-level message",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs = append(tx.msgs, tx.msgs[0])
			},
		},
		{
			name: "typed nil MsgSend",
			mutate: func(tx *standardMsgSendTestTx) {
				var msg *banktypes.MsgSend
				tx.msgs = []sdk.Msg{msg}
			},
		},
		{
			name: "non-MsgSend message",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs = []sdk.Msg{&banktypes.MsgMultiSend{}}
			},
		},
		{
			name: "MsgSend without denomination",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = nil
			},
		},
		{
			name: "MsgSend with more than one denomination",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).Amount = sdk.NewCoins(
					sdk.NewInt64Coin("denom-a", 1),
					sdk.NewInt64Coin("denom-b", 1),
				)
			},
		},
		{
			name: "fee payer differs from sender",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.payer = append(sdk.AccAddress(nil), other...)
				tx.signers = [][]byte{other}
			},
		},
		{
			name: "invalid sender encoding",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).FromAddress = "not-an-address"
			},
		},
		{
			name: "signer differs from fee payer",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signers = [][]byte{other}
			},
		},
		{
			name: "no signer",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signers = nil
			},
		},
		{
			name: "more than one signer",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signers = [][]byte{tx.payer, other}
			},
		},
		{
			name: "signer extraction failure",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signersErr = errors.New("signer failure")
			},
		},
		{
			name: "no signature group",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signatures = nil
			},
		},
		{
			name: "more than one signature group",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signatures = append(tx.signatures, signing.SignatureV2{})
			},
		},
		{
			name: "signature extraction failure",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.signaturesErr = errors.New("signature failure")
			},
		},
		{
			name: "no public key slot",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeys = nil
			},
		},
		{
			name: "more than one public key slot",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeys = append(tx.pubKeys, standardMsgSendLeafKey(2))
			},
		},
		{
			name: "public key extraction failure",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeysErr = errors.New("public key failure")
			},
		},
		{
			name: "seven signature leaves",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeys = []cryptotypes.PubKey{standardMsgSendMultisig(7)}
			},
			wantStandard: false,
		},
		{
			name: "eight signature leaves",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.pubKeys = []cryptotypes.PubKey{standardMsgSendMultisig(8)}
			},
		},
		{
			name: "different recipient remains eligible",
			mutate: func(tx *standardMsgSendTestTx) {
				tx.msgs[0].(*banktypes.MsgSend).ToAddress = otherAddress
			},
			wantStandard: true,
		},
	}

	keeper := &standardMsgSendAccountKeeper{addressCodec: codec}
	decorator := NewStandardMsgSendGasDecorator(keeper)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := newStandardMsgSendTestTx(t, codec, payer)
			if test.mutate != nil {
				test.mutate(&tx)
			}
			txBytes := test.txBytes
			if txBytes == nil {
				txBytes = []byte{0x01}
			}
			originalMeter := storetypes.NewInfiniteGasMeter()
			ctx := routerContext(false).WithGasMeter(originalMeter).WithTxBytes(txBytes)
			nextCalled := false

			newCtx, err := decorator.AnteHandle(ctx, tx, test.simulate, func(
				ctx sdk.Context,
				_ sdk.Tx,
				_ bool,
			) (sdk.Context, error) {
				nextCalled = true
				return ctx, nil
			})

			if test.wantErr {
				require.Error(t, err)
				require.False(t, nextCalled)
				require.Same(t, originalMeter, newCtx.GasMeter())
				return
			}

			require.NoError(t, err)
			require.True(t, nextCalled)
			_, standardized := newCtx.GasMeter().(*standardMsgSendGasMeter)
			require.Equal(t, test.wantStandard, standardized)
			if test.wantStandard {
				require.Equal(t, StandardMsgSendGas, uint64(newCtx.GasMeter().GasConsumed()))
				require.Equal(t, StandardMsgSendGas, uint64(newCtx.GasMeter().Limit()))
			} else {
				require.Same(t, originalMeter, newCtx.GasMeter())
			}
		})
	}
}

func TestStandardMsgSendGasDecoratorUsesStoredPublicKey(t *testing.T) {
	configureRouterEVM(t)

	codec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	tx := newStandardMsgSendTestTx(t, codec, payer)
	tx.pubKeys = []cryptotypes.PubKey{nil}
	account := authtypes.NewBaseAccountWithAddress(payer)
	require.NoError(t, account.SetPubKey(standardMsgSendLeafKey(1)))

	tests := []struct {
		name         string
		account      sdk.AccountI
		wantStandard bool
	}{
		{name: "stored key", account: account, wantStandard: true},
		{name: "missing account"},
		{name: "account without key", account: authtypes.NewBaseAccountWithAddress(payer)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			keeper := &standardMsgSendAccountKeeper{
				addressCodec: codec,
				account:      test.account,
			}
			ctx := routerContext(false).
				WithGasMeter(storetypes.NewInfiniteGasMeter()).
				WithTxBytes([]byte{0x01})

			newCtx, err := NewStandardMsgSendGasDecorator(keeper).AnteHandle(
				ctx,
				tx,
				false,
				func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil },
			)

			require.NoError(t, err)
			_, standardized := newCtx.GasMeter().(*standardMsgSendGasMeter)
			require.Equal(t, test.wantStandard, standardized)
		})
	}
}

func TestStandardMsgSendGasDecoratorRequiresConfiguredAccountKeeper(t *testing.T) {
	configureRouterEVM(t)

	codec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	tx := newStandardMsgSendTestTx(t, codec, payer)
	ctx := routerContext(false).
		WithGasMeter(storetypes.NewInfiniteGasMeter()).
		WithTxBytes([]byte{0x01})
	nextCalled := false

	newCtx, err := NewStandardMsgSendGasDecorator(nil).AnteHandle(
		ctx,
		tx,
		false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		},
	)

	require.ErrorIs(t, err, sdkerrors.ErrLogic)
	require.False(t, nextCalled)
	require.Same(t, ctx.GasMeter(), newCtx.GasMeter())
}

func TestStandardMsgSendGasDecoratorFallsBackForNilAndNonFeeTransactions(t *testing.T) {
	configureRouterEVM(t)

	codec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	base := newStandardMsgSendTestTx(t, codec, payer)
	decorator := NewStandardMsgSendGasDecorator(&standardMsgSendAccountKeeper{addressCodec: codec})

	tests := []struct {
		name string
		tx   sdk.Tx
	}{
		{name: "nil transaction"},
		{name: "transaction without fee interface", tx: standardMsgSendBareTx{msgs: base.msgs}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			meter := storetypes.NewInfiniteGasMeter()
			ctx := routerContext(false).WithGasMeter(meter).WithTxBytes([]byte{0x01})
			nextCalled := false

			newCtx, err := decorator.AnteHandle(
				ctx,
				test.tx,
				false,
				func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
					nextCalled = true
					return ctx, nil
				},
			)

			require.NoError(t, err)
			require.True(t, nextCalled)
			require.Same(t, meter, newCtx.GasMeter())
		})
	}
}

func TestHasBoundedStandardMsgSendPubKey(t *testing.T) {
	leaves := standardMsgSendLeafKeys(8)
	flatSeven := cryptomultisig.NewLegacyAminoPubKey(7, leaves[:7])
	flatEight := cryptomultisig.NewLegacyAminoPubKey(8, leaves)
	oneNestedLevel := cryptomultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{leaves[0]})
	allowedDepth := cryptomultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{oneNestedLevel})
	twoNestedLevels := cryptomultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{allowedDepth})
	sevenGroups := make([]cryptotypes.PubKey, 7)
	for index := range sevenGroups {
		sevenGroups[index] = cryptomultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{leaves[index]})
	}
	maxVisitedTree := cryptomultisig.NewLegacyAminoPubKey(len(sevenGroups), sevenGroups)
	eightNestedLeaves := cryptomultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{
		cryptomultisig.NewLegacyAminoPubKey(4, leaves[:4]),
		cryptomultisig.NewLegacyAminoPubKey(4, leaves[4:]),
	})

	tests := []struct {
		name string
		key  cryptotypes.PubKey
		want bool
	}{
		{name: "nil", key: nil},
		{name: "single leaf", key: leaves[0], want: true},
		{name: "unsupported ed25519 leaf", key: ed25519.GenPrivKey().PubKey()},
		{name: "flat seven leaves", key: flatSeven},
		{name: "flat eight leaves", key: flatEight},
		{name: "leaves at depth two", key: allowedDepth},
		{name: "maximum nodes and seven leaves", key: maxVisitedTree},
		{name: "eight leaves split below root", key: eightNestedLeaves},
		{name: "leaf below depth two", key: twoNestedLevels},
		{name: "empty multisig", key: &cryptomultisig.LegacyAminoPubKey{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, hasBoundedStandardMsgSendPubKey(test.key))
		})
	}
}

func TestStandardMsgSendGasMeterProjectsPublicGasAndRetainsInternalAccounting(t *testing.T) {
	const actualBudget uint64 = 1_000_000
	actual := storetypes.NewGasMeter(actualBudget)
	meter := newStandardMsgSendGasMeter(actual)
	standardMeter, ok := meter.(*standardMsgSendGasMeter)
	require.True(t, ok)

	require.Equal(t, StandardMsgSendGas, uint64(meter.GasConsumed()))
	require.Equal(t, StandardMsgSendGas, uint64(meter.GasConsumedToLimit()))
	require.Equal(t, StandardMsgSendGas, uint64(meter.Limit()))
	require.Equal(t, actualBudget, uint64(meter.GasRemaining()))
	require.Zero(t, standardMeter.actualGasConsumed())
	require.False(t, meter.IsOutOfGas())
	require.False(t, meter.IsPastLimit())

	meter.ConsumeGas(1_000, "bounded work")
	require.Equal(t, uint64(1_000), uint64(standardMeter.actualGasConsumed()))
	require.Equal(t, StandardMsgSendGas, uint64(meter.GasConsumed()))
	require.Equal(t, actualBudget-1_000, uint64(meter.GasRemaining()))

	meter.RefundGas(400, "bounded refund")
	require.Equal(t, uint64(600), uint64(standardMeter.actualGasConsumed()))
	require.Contains(t, meter.String(), "public: 21000")
}

func TestStandardMsgSendGasDecoratorAllowsPostClassifyWorkWithoutInternalCap(t *testing.T) {
	configureRouterEVM(t)

	codec := evmaddress.NewEvmCodec("guru")
	payer := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
	tx := newStandardMsgSendTestTx(t, codec, payer)
	keeper := &standardMsgSendAccountKeeper{addressCodec: codec}
	originalMeter := storetypes.NewGasMeter(123)
	originalMeter.ConsumeGas(123, "pre-classification")
	ctx := routerContext(false).
		WithGasMeter(originalMeter).
		WithTxBytes([]byte{0x01})
	nextCalled := false

	newCtx, err := NewStandardMsgSendGasDecorator(keeper).AnteHandle(
		ctx,
		tx,
		false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			ctx.GasMeter().ConsumeGas(10_000, "post-classify work")
			return ctx, nil
		},
	)

	require.True(t, nextCalled)
	require.NoError(t, err)
	standardMeter, ok := newCtx.GasMeter().(*standardMsgSendGasMeter)
	require.True(t, ok)
	require.Equal(t, StandardMsgSendGas, uint64(newCtx.GasMeter().GasConsumed()))
	require.Equal(t, StandardMsgSendGas, uint64(newCtx.GasMeter().GasConsumedToLimit()))
	require.Equal(t, StandardMsgSendGas, uint64(newCtx.GasMeter().Limit()))
	require.Equal(t, uint64(10_123), uint64(standardMeter.actualGasConsumed()))
	require.False(t, newCtx.GasMeter().IsOutOfGas())
	require.False(t, newCtx.GasMeter().IsPastLimit())
}

type standardMsgSendAccountKeeper struct {
	authante.AccountKeeper
	addressCodec coreaddress.Codec
	account      sdk.AccountI
}

func (keeper *standardMsgSendAccountKeeper) AddressCodec() coreaddress.Codec {
	return keeper.addressCodec
}

func (keeper *standardMsgSendAccountKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI {
	return keeper.account
}

type standardMsgSendTestTx struct {
	msgs                  []sdk.Msg
	fee                   sdk.Coins
	gas                   uint64
	payer                 sdk.AccAddress
	granter               sdk.AccAddress
	memo                  string
	unordered             bool
	extensions            []*codectypes.Any
	nonCriticalExtensions []*codectypes.Any
	signers               [][]byte
	pubKeys               []cryptotypes.PubKey
	signatures            []signing.SignatureV2
	signersErr            error
	pubKeysErr            error
	signaturesErr         error
}

var (
	_ authante.AccountKeeper         = (*standardMsgSendAccountKeeper)(nil)
	_ sdk.FeeTx                      = standardMsgSendTestTx{}
	_ sdk.TxWithMemo                 = standardMsgSendTestTx{}
	_ sdk.TxWithUnordered            = standardMsgSendTestTx{}
	_ authante.HasExtensionOptionsTx = standardMsgSendTestTx{}
	_ authsigning.SigVerifiableTx    = standardMsgSendTestTx{}
)

func (tx standardMsgSendTestTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (standardMsgSendTestTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx standardMsgSendTestTx) GetGas() uint64 { return tx.gas }

func (tx standardMsgSendTestTx) GetFee() sdk.Coins { return tx.fee }

func (tx standardMsgSendTestTx) FeePayer() []byte { return tx.payer }

func (tx standardMsgSendTestTx) FeeGranter() []byte { return tx.granter }

func (tx standardMsgSendTestTx) GetMemo() string { return tx.memo }

func (tx standardMsgSendTestTx) GetUnordered() bool { return tx.unordered }

func (standardMsgSendTestTx) GetTimeoutTimeStamp() (timestamp time.Time) { return timestamp }

func (tx standardMsgSendTestTx) GetExtensionOptions() []*codectypes.Any { return tx.extensions }

func (tx standardMsgSendTestTx) GetNonCriticalExtensionOptions() []*codectypes.Any {
	return tx.nonCriticalExtensions
}

func (tx standardMsgSendTestTx) GetSigners() ([][]byte, error) {
	return tx.signers, tx.signersErr
}

func (tx standardMsgSendTestTx) GetPubKeys() ([]cryptotypes.PubKey, error) {
	return tx.pubKeys, tx.pubKeysErr
}

func (tx standardMsgSendTestTx) GetSignaturesV2() ([]signing.SignatureV2, error) {
	return tx.signatures, tx.signaturesErr
}

type standardMsgSendBareTx struct {
	msgs []sdk.Msg
}

func (tx standardMsgSendBareTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (standardMsgSendBareTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func newStandardMsgSendTestTx(
	t *testing.T,
	codec coreaddress.Codec,
	payer sdk.AccAddress,
) standardMsgSendTestTx {
	t.Helper()

	from, err := codec.BytesToString(payer)
	require.NoError(t, err)
	to, err := codec.BytesToString(sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20)))
	require.NoError(t, err)
	pubKey := standardMsgSendLeafKey(1)

	return standardMsgSendTestTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{
			FromAddress: from,
			ToAddress:   to,
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("send-denom", 1)),
		}},
		fee:        sdk.NewCoins(sdk.NewInt64Coin(evmtypes.GetEVMCoinDenom(), 1)),
		gas:        StandardMsgSendGas,
		payer:      append(sdk.AccAddress(nil), payer...),
		signers:    [][]byte{append([]byte(nil), payer...)},
		pubKeys:    []cryptotypes.PubKey{pubKey},
		signatures: []signing.SignatureV2{{PubKey: pubKey}},
	}
}

func standardMsgSendLeafKey(index byte) cryptotypes.PubKey {
	secret := append([]byte("standard-msgsend-key"), index)
	return secp256k1.GenPrivKeyFromSecret(secret).PubKey()
}

func standardMsgSendLeafKeys(count int) []cryptotypes.PubKey {
	keys := make([]cryptotypes.PubKey, count)
	for index := range keys {
		keys[index] = standardMsgSendLeafKey(byte(index + 1))
	}
	return keys
}

func standardMsgSendMultisig(leaves int) *cryptomultisig.LegacyAminoPubKey {
	return cryptomultisig.NewLegacyAminoPubKey(leaves, standardMsgSendLeafKeys(leaves))
}
