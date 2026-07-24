package feepolicy

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	"cosmossdk.io/core/appmodule"
	coregenesis "cosmossdk.io/core/genesis"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/stretchr/testify/require"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
	feepolicykeeper "github.com/gurufinglobal/guru/v3/x/feepolicy/keeper"
	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

type genesisFixture struct {
	ctx                sdk.Context
	keeper             feepolicykeeper.Keeper
	constitutionKeeper *mockGenesisConstitutionKeeper
	moderator          string
}

type mockGenesisConstitutionKeeper struct {
	moderatorAddress string
	updates          []string
}

func (m *mockGenesisConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	return m.moderatorAddress, nil
}

func (m *mockGenesisConstitutionKeeper) UpdateModeratorAddress(_ context.Context, moderatorAddress string) error {
	m.updates = append(m.updates, moderatorAddress)
	m.moderatorAddress = moderatorAddress
	return nil
}

func TestDefaultGenesisValidatesAndInitializes(t *testing.T) {
	f := setupGenesisKeeper(t)
	am := NewAppModule(f.keeper)
	target := &coregenesis.RawJSONTarget{}
	require.NoError(t, am.DefaultGenesis(target.Target()))
	raw, err := target.JSON()
	require.NoError(t, err)
	source, err := coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)
	require.NoError(t, am.ValidateGenesis(source))

	source, err = coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)
	require.NoError(t, am.InitGenesis(f.ctx, source))
	moderator, err := f.keeper.GetModeratorAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, f.moderator, moderator)
	require.Empty(t, f.constitutionKeeper.updates, "empty legacy moderator must inherit, never update Constitution")
}

func TestGenesisRoundTripCanonicalizesAccountAndGlobalPolicies(t *testing.T) {
	f := setupGenesisKeeper(t)
	accountBytes := bytes.Repeat([]byte{0x02}, 20)
	accountHex := "0x0202020202020202020202020202020202020202"
	accountCanonical, err := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr).BytesToString(accountBytes)
	require.NoError(t, err)
	msgType := sdk.MsgTypeURL(&banktypes.MsgSend{})
	data := types.GenesisState{
		// Empty legacy moderator intentionally inherits Constitution.
		Discounts: []types.AccountDiscount{
			{
				Address: accountHex,
				Modules: []types.ModuleDiscount{{
					Module: "bank",
					Discounts: []types.Discount{{
						DiscountType: types.FeeDiscountTypePercent,
						MsgType:      msgType,
						Amount:       sdkmath.LegacyMustNewDecFromStr("25.5"),
					}},
				}},
			},
			{
				Address: "",
				Modules: []types.ModuleDiscount{{
					Module: "bank",
					Discounts: []types.Discount{{
						DiscountType: types.FeeDiscountTypeFixed,
						MsgType:      msgType,
						Amount:       sdkmath.LegacyMustNewDecFromStr("3.75"),
					}},
				}},
			},
		},
	}
	require.NoError(t, InitGenesis(f.ctx, f.keeper, data))

	exported, err := ExportGenesis(f.ctx, f.keeper)
	require.NoError(t, err)
	require.Equal(t, f.moderator, exported.ModeratorAddress)
	require.Len(t, exported.Discounts, 2)
	byAddress := make(map[string]types.AccountDiscount, len(exported.Discounts))
	for _, discount := range exported.Discounts {
		byAddress[discount.Address] = discount
	}
	require.Contains(t, byAddress, accountCanonical)
	require.Contains(t, byAddress, "")

	target := &coregenesis.RawJSONTarget{}
	require.NoError(t, writeGenesisState(target.Target(), &exported))
	raw, err := target.JSON()
	require.NoError(t, err)
	source, err := coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)

	f2 := setupGenesisKeeper(t)
	am2 := NewAppModule(f2.keeper)
	require.NoError(t, am2.InitGenesis(f2.ctx, source))
	reExported, err := ExportGenesis(f2.ctx, f2.keeper)
	require.NoError(t, err)
	require.Equal(t, exported, reExported)
}

func TestGenesisModeratorMismatchRejectsBeforePolicyImport(t *testing.T) {
	f := setupGenesisKeeper(t)
	data := types.GenesisState{
		ModeratorAddress: testGenesisHexAddress(0x04),
		Discounts: []types.AccountDiscount{{
			Address: testGenesisHexAddress(0x03),
			Modules: []types.ModuleDiscount{{
				Module: "bank",
				Discounts: []types.Discount{{
					DiscountType: types.FeeDiscountTypePercent,
					MsgType:      sdk.MsgTypeURL(&banktypes.MsgSend{}),
					Amount:       sdkmath.LegacyNewDec(25),
				}},
			}},
		}},
	}
	require.Error(t, InitGenesis(f.ctx, f.keeper, data))
	_, found, err := f.keeper.GetAccountDiscounts(f.ctx, testGenesisHexAddress(0x03))
	require.NoError(t, err)
	require.False(t, found)
}

func TestValidateGenesisRejectsInvalidState(t *testing.T) {
	validRule := types.Discount{
		DiscountType: types.FeeDiscountTypePercent,
		MsgType:      sdk.MsgTypeURL(&banktypes.MsgSend{}),
		Amount:       sdkmath.LegacyNewDec(25),
	}
	policy := func(address, module string, rules ...types.Discount) types.AccountDiscount {
		return types.AccountDiscount{
			Address: address,
			Modules: []types.ModuleDiscount{{Module: module, Discounts: rules}},
		}
	}
	wrongHRP, err := evmaddress.NewEvmCodec("wrong").BytesToString(bytes.Repeat([]byte{0x04}, 20))
	require.NoError(t, err)

	for _, tc := range []struct {
		name            string
		state           *types.GenesisState
		wantErrContains string
	}{
		{
			name:  "wrong moderator HRP",
			state: &types.GenesisState{ModeratorAddress: wrongHRP},
		},
		{
			name: "Hex and Bech32 account aliases",
			state: &types.GenesisState{
				Discounts: []types.AccountDiscount{
					policy(testGenesisHexAddress(0x03), "bank", validRule),
					policy(testGenesisCanonicalAddress(t, 0x03), "staking", validRule),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupGenesisKeeper(t)
			err := NewAppModule(f.keeper).ValidateGenesis(genesisSourceFromState(t, tc.state))
			require.Error(t, err)
			if tc.wantErrContains != "" {
				require.ErrorContains(t, err, tc.wantErrContains)
			}
		})
	}
}

func TestValidateGenesisRejectsUnknownPolicyFields(t *testing.T) {
	f := setupGenesisKeeper(t)
	am := NewAppModule(f.keeper)
	source, err := coregenesis.SourceFromRawJSON([]byte(`{
		"discounts":[{
			"adress":"0x0202020202020202020202020202020202020202",
			"modules":[{"module":"bank","discounts":[{
				"discount_type":"percent",
				"msg_type":"/cosmos.bank.v1beta1.MsgSend",
				"amount":"25.000000000000000000"
			}]}]
		}]
	}`))
	require.NoError(t, err)
	require.Error(t, am.ValidateGenesis(source), "an address typo must not silently create a global policy")
}

func setupGenesisKeeper(t *testing.T) genesisFixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_feepolicy_genesis_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	addressCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	moderator := testGenesisCanonicalAddress(t, 0x02)
	constitutionKeeper := &mockGenesisConstitutionKeeper{moderatorAddress: moderator}
	k := feepolicykeeper.NewKeeper(
		runtime.NewKVStoreService(key),
		codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
		addressCodec,
		nil,
		constitutionKeeper,
	)
	return genesisFixture{
		ctx:                testCtx.Ctx,
		keeper:             k,
		constitutionKeeper: constitutionKeeper,
		moderator:          moderator,
	}
}

func testGenesisCanonicalAddress(t *testing.T, b byte) string {
	t.Helper()
	address, err := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr).BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}

func testGenesisHexAddress(b byte) string {
	return "0x" + hex.EncodeToString(bytes.Repeat([]byte{b}, 20))
}

func genesisSourceFromState(t *testing.T, state *types.GenesisState) appmodule.GenesisSource {
	t.Helper()
	target := &coregenesis.RawJSONTarget{}
	require.NoError(t, writeGenesisState(target.Target(), state))
	raw, err := target.JSON()
	require.NoError(t, err)
	source, err := coregenesis.SourceFromRawJSON(raw)
	require.NoError(t, err)
	return source
}
