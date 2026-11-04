package app

import (
	"context"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/cosmos/evm/ethereum/eip712"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	appante "github.com/gurufinglobal/guru/v2/app/ante"
	"github.com/gurufinglobal/guru/v2/config"
)

// Called from the single application harness because Cosmos EVM configuration
// is process-global. Each ante invocation uses a discarded cache, not CheckTx.
func testEIP712AnteIntegration(t *testing.T, application *App, key *ethsecp256k1.PrivKey) {
	t.Helper()
	base := committedContext(t, application).WithChainID(application.ChainID())
	require.Positive(t, base.BlockHeight())
	require.False(t, base.BlockTime().IsZero())
	sender := sdk.AccAddress(key.PubKey().Address())
	senderText, err := application.AccountKeeper.AddressCodec().BytesToString(sender)
	require.NoError(t, err)
	account := application.AccountKeeper.GetAccount(base, sender)
	require.NotNil(t, account)
	beforeBalance := application.BankKeeper.GetBalance(base, sender, config.BaseDenom)
	beforeSequence := account.GetSequence()
	accountNumber := account.GetAccountNumber()
	future := base.BlockTime().Add(time.Hour)
	params := application.FeeMarketKeeper.GetParams(base)
	price := params.MinGasPrice
	if !params.BaseFee.IsNil() && params.BaseFee.GT(price) {
		price = params.BaseFee
	}
	const declaredGas uint64 = 200_000
	feeAmount := price.MulInt(sdkmath.NewIntFromUint64(declaredGas)).Ceil().RoundInt().AddRaw(1_000_000)
	fees := sdk.NewCoins(sdk.NewCoin(config.BaseDenom, feeAmount))
	require.True(t, beforeBalance.Amount.GT(feeAmount.AddRaw(2)))
	signerData := authsigning.SignerData{
		Address:       senderText,
		ChainID:       application.ChainID(),
		AccountNumber: accountNumber,
		Sequence:      beforeSequence,
		PubKey:        key.PubKey(),
	}
	const mode = signing.SignMode_SIGN_MODE_DIRECT
	for _, route := range []struct {
		name     string
		msgCount int
		fixed    bool
	}{
		{name: "FixedSendGas", msgCount: 1, fixed: true},
		{name: "ordinary two MsgSend", msgCount: 2, fixed: false},
	} {
		t.Run(route.name, func(t *testing.T) {
			newBuilder := func(withTimestamp bool) client.TxBuilder {
				builder := application.GetTxConfig().NewTxBuilder()
				messages := make([]sdk.Msg, route.msgCount)
				for i := range messages {
					messages[i] = banktypes.NewMsgSend(sender, sender, sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, 1)))
				}
				require.NoError(t, builder.SetMsgs(messages...))
				builder.SetFeeAmount(fees)
				builder.SetGasLimit(declaredGas)
				builder.SetUnordered(false)
				// Set the timestamp before sign bytes can cache the transaction body.
				if withTimestamp {
					builder.SetTimeoutTimestamp(future)
				}
				require.NoError(t, builder.SetSignatures(signing.SignatureV2{
					PubKey: key.PubKey(), Data: &signing.SingleSignatureData{SignMode: mode}, Sequence: beforeSequence,
				}))
				return builder
			}
			signBytes := func(builder client.TxBuilder) []byte {
				bz, err := authsigning.GetSignBytesAdapter(context.Background(), application.GetTxConfig().SignModeHandler(), mode, signerData, builder.GetTx())
				require.NoError(t, err)
				return bz
			}
			setSignature := func(builder client.TxBuilder, signature []byte) {
				require.NoError(t, builder.SetSignatures(signing.SignatureV2{
					PubKey: key.PubKey(), Data: &signing.SingleSignatureData{SignMode: mode, Signature: signature}, Sequence: beforeSequence,
				}))
			}
			runAnte := func(t *testing.T, builder client.TxBuilder, wantUnauthorized bool) {
				t.Helper()
				encoded, err := application.GetTxConfig().TxEncoder()(builder.GetTx())
				require.NoError(t, err)
				decoded, err := application.GetTxConfig().TxDecoder()(encoded)
				require.NoError(t, err)
				cached, _ := base.CacheContext()
				cached = cached.WithChainID(application.ChainID()).WithIsCheckTx(true).WithExecMode(sdk.ExecModeCheck).
					WithIsSigverifyTx(true).WithTxBytes(encoded).WithMinGasPrices(sdk.DecCoins{}).
					WithGasMeter(storetypes.NewInfiniteGasMeter()).WithBlockGasMeter(storetypes.NewInfiniteGasMeter()).
					WithEventManager(sdk.NewEventManager())
				result, err := application.anteHandler(cached, decoded, false)
				// Also verify the rejected mutation passed through the intended Guru route.
				if route.fixed {
					require.Equal(t, appante.StandardMsgSendGas, result.GasMeter().Limit())
				} else {
					require.Equal(t, declaredGas, result.GasMeter().Limit())
				}
				if wantUnauthorized {
					require.ErrorIs(t, err, sdkerrors.ErrUnauthorized, "mutation must reach signature rejection")
				} else {
					require.NoError(t, err)
					if route.fixed {
						require.Equal(t, appante.StandardMsgSendGas, result.GasMeter().GasConsumed())
					}
					require.Equal(t, beforeSequence+1, application.AccountKeeper.GetAccount(result, sender).GetSequence())
				}
				// Never write the branch back: ante may deduct fees before rejecting a
				// signature. This checks cache isolation, not BaseApp rollback behavior.
				require.Equal(t, beforeBalance, application.BankKeeper.GetBalance(base, sender, config.BaseDenom))
				require.Equal(t, beforeSequence, application.AccountKeeper.GetAccount(base, sender).GetSequence())
			}
			clean := newBuilder(false)
			cleanDirectBytes := signBytes(clean)
			typedRawBytes, err := eip712.GetEIP712BytesForMsg(cleanDirectBytes)
			require.NoError(t, err)
			typedSignature, err := key.Sign(typedRawBytes)
			require.NoError(t, err)
			require.True(t, key.PubKey().VerifySignature(cleanDirectBytes, typedSignature))
			setSignature(clean, typedSignature)
			t.Run("clean_EIP712_is_accepted", func(t *testing.T) { runAnte(t, clean, false) })
			mutant := newBuilder(true)
			mutantDirectBytes := signBytes(mutant)
			require.NotEqual(t, cleanDirectBytes, mutantDirectBytes)
			_, err = eip712.GetEIP712BytesForMsg(mutantDirectBytes)
			require.ErrorContains(t, err, "unsupported fields")
			require.False(t, key.PubKey().VerifySignature(mutantDirectBytes, typedSignature))
			setSignature(mutant, typedSignature)
			t.Run("same_signature_timestamp_mutant_is_rejected", func(t *testing.T) { runAnte(t, mutant, true) })
			direct := newBuilder(true)
			directSignature, err := key.Sign(signBytes(direct))
			require.NoError(t, err)
			setSignature(direct, directSignature)
			t.Run("properly_signed_native_DIRECT_timestamp_is_accepted", func(t *testing.T) { runAnte(t, direct, false) })
		})
	}
}

// testStakingAccountDeletionIntegration connects the real Guru staking store to
// the EVM deletion guard. It uses keeper operations, not a SELFDESTRUCT opcode.
func testStakingAccountDeletionIntegration(t *testing.T, application *App, base sdk.Context, funder sdk.AccAddress) {
	t.Helper()
	const accountSequence uint64 = 7
	addr := common.HexToAddress("0x00000000000000000000000000000000000064c6")
	cosmosAddr := sdk.AccAddress(addr.Bytes())
	code := []byte{0x00}
	codeHash := ethcrypto.Keccak256Hash(code)
	storageKey := common.HexToHash("0x01")
	storageValue := common.HexToHash("0x6464")
	funderBalance := application.BankKeeper.GetBalance(base, funder, config.BaseDenom)
	require.Nil(t, application.AccountKeeper.GetAccount(base, cosmosAddr))

	for _, hasPosition := range []bool{false, true} {
		name := "without staking position"
		if hasPosition {
			name = "with real unbonding delegation"
		}
		t.Run(name, func(t *testing.T) {
			ctx, _ := base.CacheContext()
			account := application.AccountKeeper.NewAccountWithAddress(ctx, cosmosAddr)
			require.NoError(t, account.SetSequence(accountSequence))
			application.AccountKeeper.SetAccount(ctx, account)
			accountNumber := account.GetAccountNumber()
			application.EVMKeeper.SetCodeHash(ctx, addr.Bytes(), codeHash.Bytes())
			application.EVMKeeper.SetCode(ctx, codeHash.Bytes(), code)
			application.EVMKeeper.SetState(ctx, addr, storageKey, storageValue.Bytes())

			if hasPosition {
				validators, err := application.StakingKeeper.GetAllValidators(ctx)
				require.NoError(t, err)
				require.Len(t, validators, 1)
				validator := validators[0]
				require.True(t, validator.IsBonded())
				valAddr, err := application.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.OperatorAddress)
				require.NoError(t, err)
				require.NoError(t, application.BankKeeper.SendCoins(ctx, funder, cosmosAddr,
					sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, 15))))
				shares, err := application.CustomStakingKeeper.Delegate(ctx, cosmosAddr, sdkmath.NewInt(10),
					stakingtypes.Unbonded, validator, true)
				require.NoError(t, err)
				completionTime, amount, err := application.CustomStakingKeeper.Undelegate(ctx, cosmosAddr, sdk.ValAddress(valAddr), shares)
				require.NoError(t, err)
				require.True(t, amount.IsPositive())
				require.True(t, completionTime.After(ctx.BlockTime()))
			}
			// No live delegation or redelegation may short-circuit the UBD query.
			delegations, err := application.StakingKeeper.GetAllDelegatorDelegations(ctx, cosmosAddr)
			require.NoError(t, err)
			require.Empty(t, delegations)
			redelegations, err := application.StakingKeeper.GetRedelegations(ctx, cosmosAddr, 1)
			require.NoError(t, err)
			require.Empty(t, redelegations)
			beforeClaims, err := application.CustomStakingKeeper.GetAllUnbondingDelegations(ctx, cosmosAddr)
			require.NoError(t, err)
			baseClaims, err := application.StakingKeeper.GetAllUnbondingDelegations(ctx, cosmosAddr)
			require.NoError(t, err)
			require.Equal(t, beforeClaims, baseClaims)
			beforeBalance := application.BankKeeper.GetBalance(ctx, cosmosAddr, config.BaseDenom)

			err = application.EVMKeeper.DeleteAccount(ctx, addr)
			if hasPosition {
				require.Len(t, beforeClaims, 1)
				require.Len(t, beforeClaims[0].Entries, 1)
				require.True(t, beforeBalance.IsPositive())
				require.ErrorContains(t, err, "staking positions")
				retained := application.AccountKeeper.GetAccount(ctx, cosmosAddr)
				require.NotNil(t, retained)
				require.Equal(t, accountNumber, retained.GetAccountNumber())
				require.Equal(t, accountSequence, retained.GetSequence())
				require.Equal(t, beforeBalance, application.BankKeeper.GetBalance(ctx, cosmosAddr, config.BaseDenom))
				require.Equal(t, codeHash, application.EVMKeeper.GetCodeHash(ctx, addr))
				require.Equal(t, code, application.EVMKeeper.GetCode(ctx, codeHash))
				require.Equal(t, storageValue, application.EVMKeeper.GetState(ctx, addr, storageKey))
				afterClaims, queryErr := application.CustomStakingKeeper.GetAllUnbondingDelegations(ctx, cosmosAddr)
				require.NoError(t, queryErr)
				require.Equal(t, beforeClaims, afterClaims)
			} else {
				require.Empty(t, beforeClaims)
				require.NoError(t, err)
				require.Nil(t, application.AccountKeeper.GetAccount(ctx, cosmosAddr))
				require.Equal(t, common.BytesToHash(evmtypes.EmptyCodeHash), application.EVMKeeper.GetCodeHash(ctx, addr))
				require.Equal(t, common.Hash{}, application.EVMKeeper.GetState(ctx, addr, storageKey))
			}
			// Both outcomes remain inside their discarded branch.
			require.Nil(t, application.AccountKeeper.GetAccount(base, cosmosAddr))
			require.Equal(t, funderBalance, application.BankKeeper.GetBalance(base, funder, config.BaseDenom))
		})
	}
}
