package keeper

import (
	"bytes"
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
)

func TestValidateTxSelfBondConstraintsAdditionalScenarios(t *testing.T) {
	f := setupSelfBondFixture(t)
	require.NoError(t, f.keeper.SetParams(f.ctx, testParams("100")))

	_, validatorAddress, selfDelegatorAddress := f.addValidatorWithSelfBond(t, 0x41, stakingtypes.Bonded, 120)
	otherDelegatorAddress, err := f.accountCodec.BytesToString(bytes.Repeat([]byte{0x42}, 20))
	require.NoError(t, err)

	tests := []struct {
		name      string
		tx        sdk.Tx
		shouldErr bool
	}{
		{"nil tx is ignored", nil, false},
		{
			name: "allows create validator with non-base denom",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCreateValidator{
					ValidatorAddress: validatorAddress,
					Value: sdk.Coin{
						Denom:  "uatom",
						Amount: mustInt(t, "1"),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "rejects self begin redelegate below minimum",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgBeginRedelegate{
					DelegatorAddress:    selfDelegatorAddress,
					ValidatorSrcAddress: validatorAddress,
					ValidatorDstAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "30"),
					},
				},
			}},
			shouldErr: true,
		},
		{
			name: "allows self undelegate with non-base denom",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  "uatom",
						Amount: mustInt(t, "1000000"),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "allows non-self begin redelegate",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgBeginRedelegate{
					DelegatorAddress:    otherDelegatorAddress,
					ValidatorSrcAddress: validatorAddress,
					ValidatorDstAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "30"),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "rejects cumulative self undelegations in same tx",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "15"),
					},
				},
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "15"),
					},
				},
			}},
			shouldErr: true,
		},
		{
			name: "tracks self delegate increase then undelegate decrease",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgDelegate{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "20"),
					},
				},
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "40"),
					},
				},
			}},
			shouldErr: false,
		},
		{
			name: "tracks self cancel unbonding as self-bond increase",
			tx: testTx{msgs: []sdk.Msg{
				&stakingtypes.MsgCancelUnbondingDelegation{
					DelegatorAddress: selfDelegatorAddress,
					ValidatorAddress: validatorAddress,
					Amount: sdk.Coin{
						Denom:  appparams.BaseDenom,
						Amount: mustInt(t, "1"),
					},
					CreationHeight: 1,
				},
			}},
			shouldErr: false,
		},
		{
			name: "fails on malformed nested authz messages",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						{TypeUrl: "/unknown.Msg"},
					},
				},
			}},
			shouldErr: true,
		},
		{
			name: "rejects cumulative self undelegations inside authz exec",
			tx: testTx{msgs: []sdk.Msg{
				&authztypes.MsgExec{
					Grantee: "grantee",
					Msgs: []*codectypes.Any{
						mustAnyWithValue(t, &stakingtypes.MsgUndelegate{
							DelegatorAddress: selfDelegatorAddress,
							ValidatorAddress: validatorAddress,
							Amount: sdk.Coin{
								Denom:  appparams.BaseDenom,
								Amount: mustInt(t, "15"),
							},
						}),
						mustAnyWithValue(t, &stakingtypes.MsgUndelegate{
							DelegatorAddress: selfDelegatorAddress,
							ValidatorAddress: validatorAddress,
							Amount: sdk.Coin{
								Denom:  appparams.BaseDenom,
								Amount: mustInt(t, "15"),
							},
						}),
					},
				},
			}},
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := f.keeper.ValidateTxSelfBondConstraints(f.ctx, tc.tx, f.accountCodec)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateTxSelfBondConstraintsMissingValidatorUndelegate(t *testing.T) {
	f := setupSelfBondFixture(t)

	missingValAddr := sdk.ValAddress(bytes.Repeat([]byte{0x50}, 20))
	validatorAddress, err := f.valCodec.BytesToString(missingValAddr)
	require.NoError(t, err)
	delegatorAddress, err := f.accountCodec.BytesToString(missingValAddr)
	require.NoError(t, err)

	tx := testTx{msgs: []sdk.Msg{
		&stakingtypes.MsgUndelegate{
			DelegatorAddress: delegatorAddress,
			ValidatorAddress: validatorAddress,
			Amount: sdk.Coin{
				Denom:  appparams.BaseDenom,
				Amount: mustInt(t, "1"),
			},
		},
	}}

	err = f.keeper.ValidateTxSelfBondConstraints(f.ctx, tx, f.accountCodec)
	require.NoError(t, err)
}

func TestEnsureValidatorMinSelfBond(t *testing.T) {
	f := setupSelfBondFixture(t)

	belowAddr, _, _ := f.addValidatorWithSelfBond(t, 0x61, stakingtypes.Bonded, 9)
	require.Error(t, f.keeper.EnsureValidatorMinSelfBond(f.ctx, belowAddr, mustInt(t, "10")))

	atAddr, _, _ := f.addValidatorWithSelfBond(t, 0x62, stakingtypes.Bonded, 10)
	require.NoError(t, f.keeper.EnsureValidatorMinSelfBond(f.ctx, atAddr, mustInt(t, "10")))
}

func TestGetValidatorSelfBondNoDelegationAndDelegationErrors(t *testing.T) {
	f := setupSelfBondFixture(t)

	valAddr := sdk.ValAddress(bytes.Repeat([]byte{0x71}, 20))
	validatorAddress, err := f.valCodec.BytesToString(valAddr)
	require.NoError(t, err)
	f.stakingKeeper.validators[string(valAddr)] = stakingtypes.Validator{
		OperatorAddress: validatorAddress,
		Status:          stakingtypes.Bonded,
		Tokens:          mustInt(t, "100"),
		DelegatorShares: mustLegacyDec(t, "100"),
	}

	selfBond, err := f.keeper.GetValidatorSelfBond(f.ctx, valAddr)
	require.NoError(t, err)
	require.True(t, selfBond.IsZero())

	f.stakingKeeper.getDelegationErr = errors.New("delegation read failed")
	_, err = f.keeper.GetValidatorSelfBond(f.ctx, valAddr)
	require.Error(t, err)
}

func TestMustValidatorAddressStringFallback(t *testing.T) {
	f := setupSelfBondFixture(t)
	f.stakingKeeper.valCodec = failingCodec{}

	out := f.keeper.mustValidatorAddressString(sdk.ValAddress(bytes.Repeat([]byte{0x81}, 20)))
	require.Equal(t, "<invalid-validator-address>", out)
}

func mustLegacyDec(t *testing.T, amount string) sdkmath.LegacyDec {
	t.Helper()

	value, err := sdkmath.LegacyNewDecFromStr(amount)
	require.NoError(t, err)
	return value
}
