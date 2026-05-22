package keeper

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"cosmossdk.io/core/address"
	sdkmath "cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

type mockMinValidatorBondSource struct {
	minBond sdkmath.Int
	err     error
}

func (m mockMinValidatorBondSource) GetMinValidatorBondAmount(context.Context) (sdkmath.Int, error) {
	if m.err != nil {
		return sdkmath.Int{}, m.err
	}

	return m.minBond, nil
}

type mockSelfBondSource struct {
	valCodec    address.Codec
	validators  map[string]stakingtypes.Validator
	delegations map[string]stakingtypes.Delegation

	getValidatorErr  error
	getDelegationErr error
}

func newMockSelfBondSource(valCodec address.Codec) *mockSelfBondSource {
	return &mockSelfBondSource{
		valCodec:    valCodec,
		validators:  make(map[string]stakingtypes.Validator),
		delegations: make(map[string]stakingtypes.Delegation),
	}
}

func (m *mockSelfBondSource) GetValidator(_ context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	if m.getValidatorErr != nil {
		return stakingtypes.Validator{}, m.getValidatorErr
	}

	validator, ok := m.validators[string(addr)]
	if !ok {
		return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
	}

	return validator, nil
}

func (m *mockSelfBondSource) GetDelegation(_ context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error) {
	if m.getDelegationErr != nil {
		return stakingtypes.Delegation{}, m.getDelegationErr
	}

	key := string(delAddr) + "|" + string(valAddr)
	delegation, ok := m.delegations[key]
	if !ok {
		return stakingtypes.Delegation{}, stakingtypes.ErrNoDelegation
	}

	return delegation, nil
}

func (m *mockSelfBondSource) ValidatorAddressCodec() address.Codec {
	return m.valCodec
}

type testTx struct {
	msgs []sdk.Msg
}

func (tx testTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx testTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func mustInt(t *testing.T, amount string) sdkmath.Int {
	t.Helper()

	value, ok := sdkmath.NewIntFromString(amount)
	if !ok {
		t.Fatalf("failed to parse int: %s", amount)
	}

	return value
}

func mustAnyWithValue(t *testing.T, msg sdk.Msg) *codectypes.Any {
	t.Helper()

	any, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		t.Fatalf("failed to pack message into Any: %v", err)
	}

	return any
}

func setupAnteKeeperFixture(t *testing.T, minBond string) (*Keeper, sdk.Context, address.Codec, address.Codec, sdk.ValAddress, string, string) {
	t.Helper()

	valCodec := evmaddress.NewEvmCodec("gurvaloper")
	accountCodec := evmaddress.NewEvmCodec("gur")
	source := newMockSelfBondSource(valCodec)

	validatorAddr := sdk.ValAddress(bytes.Repeat([]byte{0x11}, 20))
	validatorAddress, err := valCodec.BytesToString(validatorAddr)
	require.NoError(t, err)
	delegatorAddress, err := accountCodec.BytesToString(validatorAddr)
	require.NoError(t, err)

	shares := sdkmath.LegacyNewDec(120)
	source.validators[string(validatorAddr)] = stakingtypes.Validator{
		OperatorAddress: validatorAddress,
		Status:          stakingtypes.Bonded,
		Tokens:          mustInt(t, "120"),
		DelegatorShares: shares,
	}
	source.delegations[string(validatorAddr)+"|"+string(validatorAddr)] = stakingtypes.Delegation{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
		Shares:           shares,
	}

	keeper := &Keeper{
		minBondSource:  mockMinValidatorBondSource{minBond: mustInt(t, minBond)},
		accountCodec:   accountCodec,
		selfBondSource: source,
	}

	return keeper, sdk.Context{}.WithContext(context.Background()), accountCodec, valCodec, validatorAddr, validatorAddress, delegatorAddress
}

func TestValidateTxSelfBondConstraints(t *testing.T) {
	keeper, ctx, accountCodec, _, _, validatorAddress, selfDelegatorAddress := setupAnteKeeperFixture(t, "100")
	otherDelegatorAddress, err := accountCodec.BytesToString(bytes.Repeat([]byte{0x22}, 20))
	require.NoError(t, err)

	tests := []struct {
		name      string
		tx        sdk.Tx
		shouldErr bool
	}{
		{"nil tx ignored", nil, false},
		{
			name: "rejects create validator below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: validatorAddress,
					Value:            sdk.Coin{Denom: appparams.BaseDenom, Amount: mustInt(t, "99")},
				},
			}},
			shouldErr: true,
		},
		{
			name: "allows create validator at minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: validatorAddress,
					Value:            sdk.Coin{Denom: appparams.BaseDenom, Amount: mustInt(t, "100")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "rejects self undelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount:           sdk.Coin{Denom: appparams.BaseDenom, Amount: mustInt(t, "30")},
				},
			}},
			shouldErr: true,
		},
		{
			name: "allows non-self undelegate",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: otherDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount:           sdk.Coin{Denom: appparams.BaseDenom, Amount: mustInt(t, "30")},
				},
			}},
			shouldErr: false,
		},
		{
			name: "rejects nested authz undelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						mustAnyWithValue(t, &stakingtypes.MsgUndelegate{
							DelegatorAddress: selfDelegatorAddress,
							ValidatorAddress: validatorAddress,
							Amount:           sdk.Coin{Denom: appparams.BaseDenom, Amount: mustInt(t, "30")},
						}),
					},
				},
			}},
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.ValidateTxSelfBondConstraints(ctx, tc.tx)
			if tc.shouldErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateTxSelfBondConstraintsHandlesMissingValidator(t *testing.T) {
	keeper, ctx, accountCodec, valCodec, _, _, _ := setupAnteKeeperFixture(t, "100")

	missingValAddr := sdk.ValAddress(bytes.Repeat([]byte{0x33}, 20))
	validatorAddress, err := valCodec.BytesToString(missingValAddr)
	require.NoError(t, err)
	delegatorAddress, err := accountCodec.BytesToString(missingValAddr)
	require.NoError(t, err)

	tx := testTx{msgs: []sdk.Msg{
		&stakingtypes.MsgUndelegate{
			DelegatorAddress: delegatorAddress,
			ValidatorAddress: validatorAddress,
			Amount:           sdk.Coin{Denom: appparams.BaseDenom, Amount: mustInt(t, "1")},
		},
	}}

	require.NoError(t, keeper.ValidateTxSelfBondConstraints(ctx, tx))
}

func TestGetValidatorSelfBond(t *testing.T) {
	keeper, ctx, _, _, validatorAddr, _, _ := setupAnteKeeperFixture(t, "100")

	selfBond, err := keeper.GetValidatorSelfBond(ctx, validatorAddr)
	require.NoError(t, err)
	require.Equal(t, "120", selfBond.String())
}

func TestGetValidatorSelfBondReturnsZeroWhenNoDelegation(t *testing.T) {
	keeper, ctx, _, _, validatorAddr, _, _ := setupAnteKeeperFixture(t, "100")
	source := keeper.selfBondSource.(*mockSelfBondSource)
	delete(source.delegations, string(validatorAddr)+"|"+string(validatorAddr))

	selfBond, err := keeper.GetValidatorSelfBond(ctx, validatorAddr)
	require.NoError(t, err)
	require.True(t, selfBond.IsZero())
}

func TestGetValidatorSelfBondPropagatesDelegationError(t *testing.T) {
	keeper, ctx, _, _, validatorAddr, _, _ := setupAnteKeeperFixture(t, "100")
	source := keeper.selfBondSource.(*mockSelfBondSource)
	source.getDelegationErr = errors.New("delegation read failed")

	_, err := keeper.GetValidatorSelfBond(ctx, validatorAddr)
	require.Error(t, err)
}
