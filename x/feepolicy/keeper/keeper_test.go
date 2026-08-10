package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	coreaddress "cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/types/query"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/stretchr/testify/require"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

type keeperFixture struct {
	ctx                sdk.Context
	keeper             Keeper
	storeService       corestore.KVStoreService
	addressCodec       coreaddress.Codec
	constitutionKeeper *mockConstitutionKeeper
	moderator          string
	account            string
	accountHex         string
}

// mockConstitutionKeeper models the shared state owner. In particular, a
// feepolicy test must never rely on a legacy local moderator record.
type mockConstitutionKeeper struct {
	moderatorAddress string
	getErr           error
	updateErr        error
	updates          []string
}

func (m *mockConstitutionKeeper) GetModeratorAddress(context.Context) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	if m.moderatorAddress == "" {
		return "", collections.ErrNotFound
	}
	return m.moderatorAddress, nil
}

func (m *mockConstitutionKeeper) UpdateModeratorAddress(_ context.Context, moderatorAddress string) error {
	m.updates = append(m.updates, moderatorAddress)
	if m.updateErr != nil {
		return m.updateErr
	}
	m.moderatorAddress = moderatorAddress
	return nil
}

func newKeeperFixture(t *testing.T, moduleKeepers map[string]types.ModuleKeeper) keeperFixture {
	t.Helper()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_feepolicy_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	addressCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	moderator := testCanonicalAddress(t, addressCodec, 0x02)
	account := testCanonicalAddress(t, addressCodec, 0x03)
	constitutionKeeper := &mockConstitutionKeeper{moderatorAddress: moderator}
	c := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	service := runtime.NewKVStoreService(key)
	k := NewKeeper(service, c, addressCodec, moduleKeepers, constitutionKeeper)
	return keeperFixture{
		ctx:                testCtx.Ctx,
		keeper:             k,
		storeService:       service,
		addressCodec:       addressCodec,
		constitutionKeeper: constitutionKeeper,
		moderator:          moderator,
		account:            account,
		accountHex:         testHexAddress(0x03),
	}
}

func testCanonicalAddress(t *testing.T, addressCodec coreaddress.Codec, b byte) string {
	t.Helper()
	address, err := addressCodec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}

func testHexAddress(b byte) string {
	return "0x" + hex.EncodeToString(bytes.Repeat([]byte{b}, 20))
}

func testPolicy(address, module string, rules ...types.Discount) types.AccountDiscount {
	return types.AccountDiscount{
		Address: address,
		Modules: []types.ModuleDiscount{{Module: module, Discounts: rules}},
	}
}

func percentRule(msg sdk.Msg, amount string) types.Discount {
	return types.Discount{
		DiscountType: types.FeeDiscountTypePercent,
		MsgType:      sdk.MsgTypeURL(msg),
		Amount:       sdkmath.LegacyMustNewDecFromStr(amount),
	}
}

func fixedRule(msg sdk.Msg, amount string) types.Discount {
	return types.Discount{
		DiscountType: types.FeeDiscountTypeFixed,
		MsgType:      sdk.MsgTypeURL(msg),
		Amount:       sdkmath.LegacyMustNewDecFromStr(amount),
	}
}

func registerOne(f *keeperFixture, moderator, address string) error {
	_, err := NewMsgServer(&f.keeper).RegisterDiscounts(f.ctx, &types.MsgRegisterDiscounts{
		ModeratorAddress: moderator,
		Discounts:        []types.AccountDiscount{testPolicy(address, "bank", percentRule(&banktypes.MsgSend{}, "25"))},
	})
	return err
}

func requirePolicyFound(t *testing.T, f *keeperFixture, address string, want bool) types.AccountDiscount {
	t.Helper()
	policy, found, err := f.keeper.GetAccountDiscounts(f.ctx, address)
	require.NoError(t, err)
	require.Equal(t, want, found)
	return policy
}

func TestRawStateLayoutAndCanonicalAlias(t *testing.T) {
	f := newKeeperFixture(t, nil)
	msg := &banktypes.MsgSend{}
	accountPolicy := testPolicy(f.accountHex, "bank", percentRule(msg, "25"))
	globalPolicy := testPolicy("", "bank", fixedRule(msg, "3"))
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, accountPolicy))
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, globalPolicy))

	stored, found, err := f.keeper.GetAccountDiscounts(f.ctx, f.accountHex)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, f.account, stored.Address)

	store := f.storeService.OpenKVStore(f.ctx)
	actual, err := store.Get([]byte{0x01})
	require.NoError(t, err)
	require.Nil(t, actual, "the former local moderator key must remain unused")

	actual, err = store.Get(append([]byte{0x02}, []byte(f.account)...))
	require.NoError(t, err)
	require.Equal(t, mustDecodeFixtureHex(t,
		"0a2b6775727531717670737871637271767073787163727176707378716372"+
			"717670737871637236617870613412450a0462616e6b123d0a077065726365"+
			"6e74121c2f636f736d6f732e62616e6b2e763162657461312e4d736753656e"+
			"641a143235303030303030303030303030303030303030",
	), actual)

	actual, err = store.Get(append([]byte{0x02}, []byte("__global__")...))
	require.NoError(t, err)
	require.Equal(t, mustDecodeFixtureHex(t,
		"12420a0462616e6b123a0a056669786564121c2f636f736d6f732e62616e6b"+
			"2e763162657461312e4d736753656e641a1333303030303030303030303030"+
			"303030303030",
	), actual)

	all, err := f.keeper.GetAllDiscounts(f.ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy(f.account, "bank", fixedRule(msg, "4"))))
	all, err = f.keeper.GetAllDiscounts(f.ctx)
	require.NoError(t, err)
	require.Len(t, all, 2, "Hex and Bech32 aliases must overwrite one record")
}

func mustDecodeFixtureHex(t *testing.T, encoded string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(encoded)
	require.NoError(t, err)
	return decoded
}

func TestAddressValidationAndGlobalNamespace(t *testing.T) {
	f := newKeeperFixture(t, nil)
	msg := &banktypes.MsgSend{}

	wrongHRPCodec := evmaddress.NewEvmCodec("wrong")
	wrongHRP := testCanonicalAddress(t, wrongHRPCodec, 0x03)
	wrongLength, err := bech32.ConvertAndEncode(appparams.Bech32PrefixAccAddr, bytes.Repeat([]byte{0x03}, 19))
	require.NoError(t, err)
	for _, address := range []string{" malformed", f.account + " ", wrongHRP, wrongLength, "__global__"} {
		err := f.keeper.SetAccountDiscounts(f.ctx, testPolicy(address, "bank", percentRule(msg, "25")))
		require.Error(t, err, address)
	}

	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy("", "bank", percentRule(msg, "25"))))
	_, found, err := f.keeper.GetAccountDiscounts(f.ctx, "")
	require.NoError(t, err)
	require.True(t, found)
	_, _, err = f.keeper.GetAccountDiscounts(f.ctx, "__global__")
	require.Error(t, err, "the sentinel must never be accepted as an external address")
}

func TestResolveDiscountPriorityCardinalityAndAuthzTopLevel(t *testing.T) {
	msg := &banktypes.MsgSend{}
	f := newKeeperFixture(t, nil)
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy(f.account, "bank", percentRule(msg, "25"))))
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy("", "bank", fixedRule(msg, "7"))))

	discount, err := f.keeper.ResolveDiscount(f.ctx, f.accountHex, []sdk.Msg{msg})
	require.NoError(t, err)
	require.Equal(t, types.FeeDiscountTypePercent, discount.DiscountType)
	require.True(t, discount.Amount.Equal(sdkmath.LegacyNewDec(25)))

	other := testCanonicalAddress(t, f.addressCodec, 0x04)
	discount, err = f.keeper.ResolveDiscount(f.ctx, other, []sdk.Msg{msg})
	require.NoError(t, err)
	require.Equal(t, types.FeeDiscountTypeFixed, discount.DiscountType)

	for _, msgs := range [][]sdk.Msg{nil, {msg, msg}} {
		discount, err = f.keeper.ResolveDiscount(f.ctx, f.account, msgs)
		require.NoError(t, err)
		require.Empty(t, discount.DiscountType)
	}

	msgExec := &authz.MsgExec{}
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy(f.account, "authz", percentRule(msgExec, "11"))))
	discount, err = f.keeper.ResolveDiscount(f.ctx, f.account, []sdk.Msg{msgExec})
	require.NoError(t, err)
	require.True(t, discount.Amount.Equal(sdkmath.LegacyNewDec(11)))
	discount, err = f.keeper.ResolveDiscount(f.ctx, f.account, []sdk.Msg{msg})
	require.NoError(t, err)
	require.Equal(t, types.FeeDiscountTypeFixed, discount.DiscountType, "account non-match falls back globally")
}

func TestStateDecodeErrorIsFailClosed(t *testing.T) {
	f := newKeeperFixture(t, nil)
	store := f.storeService.OpenKVStore(f.ctx)
	key := append([]byte{0x02}, []byte(f.account)...)
	require.NoError(t, store.Set(key, []byte{0xff}))

	_, found, err := f.keeper.GetAccountDiscounts(f.ctx, f.account)
	require.Error(t, err)
	require.False(t, found)
	_, err = f.keeper.ResolveDiscount(f.ctx, f.account, []sdk.Msg{&banktypes.MsgSend{}})
	require.Error(t, err)
}

func TestRegisterDiscountsModeratorByteAuthorization(t *testing.T) {
	f := newKeeperFixture(t, nil)
	beforeEvents := len(f.ctx.EventManager().Events())
	err := registerOne(&f, testHexAddress(0x09), f.accountHex)
	require.ErrorIs(t, err, types.ErrWrongModerator)
	requirePolicyFound(t, &f, f.account, false)
	require.Len(t, f.ctx.EventManager().Events(), beforeEvents)

	err = registerOne(&f, testHexAddress(0x02), f.accountHex)
	require.NoError(t, err, "Hex alias of the stored Bech32 moderator must authorize")
	stored := requirePolicyFound(t, &f, f.account, true)
	require.Equal(t, f.account, stored.Address)
}

func TestRegisterDiscountsRejectsInvalidBatchesAtomically(t *testing.T) {
	msg := &banktypes.MsgSend{}
	accountHex := testHexAddress(0x03)
	accountCanonical := testCanonicalAddress(t, evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr), 0x03)
	laterAddress := testHexAddress(0x04)
	for _, tc := range []struct {
		name            string
		discounts       []types.AccountDiscount
		absentAddresses []string
	}{
		{name: "empty batch"},
		{
			name: "invalid rule after valid prefix",
			discounts: []types.AccountDiscount{
				testPolicy(accountHex, "bank", percentRule(msg, "10")),
				testPolicy(laterAddress, "bank", types.Discount{
					DiscountType: types.FeeDiscountTypePercent,
					MsgType:      sdk.MsgTypeURL(msg),
					Amount:       sdkmath.LegacyZeroDec(),
				}),
			},
			absentAddresses: []string{accountHex, laterAddress},
		},
		{
			name: "Hex and Bech32 account aliases",
			discounts: []types.AccountDiscount{
				testPolicy(accountHex, "bank", percentRule(msg, "10")),
				testPolicy(accountCanonical, "staking", percentRule(msg, "20")),
			},
			absentAddresses: []string{accountHex},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newKeeperFixture(t, nil)
			beforeEvents := len(f.ctx.EventManager().Events())

			_, err := NewMsgServer(&f.keeper).RegisterDiscounts(f.ctx, &types.MsgRegisterDiscounts{
				ModeratorAddress: testHexAddress(0x02),
				Discounts:        tc.discounts,
			})
			require.Error(t, err)

			for _, address := range tc.absentAddresses {
				require.Empty(t, requirePolicyFound(t, &f, address, false), "a rejected batch must not write policy state")
			}
			require.Len(t, f.ctx.EventManager().Events(), beforeEvents, "a rejected batch must not emit an event")
		})
	}
}

func TestConstitutionModeratorIsDynamicAndLocalLegacyKeyIsIgnored(t *testing.T) {
	f := newKeeperFixture(t, nil)

	// 0x01 was the legacy local moderator key. Its value is deliberately
	// poisonous: authorization must consult Constitution only.
	store := f.storeService.OpenKVStore(f.ctx)
	require.NoError(t, store.Set([]byte{0x01}, []byte("not-a-moderator")))
	err := registerOne(&f, testHexAddress(0x02), f.account)
	require.NoError(t, err, "Hex alias of Constitution moderator must authorize")

	f.constitutionKeeper.moderatorAddress = testCanonicalAddress(t, f.addressCodec, 0x05)
	err = registerOne(&f, f.moderator, testHexAddress(0x04))
	require.ErrorIs(t, err, types.ErrWrongModerator, "old Constitution moderator must lose authorization immediately")
	err = registerOne(&f, testHexAddress(0x05), testHexAddress(0x04))
	require.NoError(t, err, "rotated Constitution moderator must authorize by Hex/Bech32 byte equality")
}

func TestConstitutionModeratorFailuresAreFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*mockConstitutionKeeper)
		wantErrIs error
	}{
		{
			name:      "missing moderator",
			configure: func(m *mockConstitutionKeeper) { m.moderatorAddress = "" },
			wantErrIs: types.ErrWrongModerator,
		},
		{
			name:      "arbitrary Constitution read error",
			configure: func(m *mockConstitutionKeeper) { m.getErr = errors.New("constitution unavailable") },
		},
		{
			name:      "malformed configured moderator",
			configure: func(m *mockConstitutionKeeper) { m.moderatorAddress = "not-an-address" },
			wantErrIs: types.ErrWrongModerator,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newKeeperFixture(t, nil)
			tc.configure(f.constitutionKeeper)
			err := registerOne(&f, testHexAddress(0x02), f.account)
			require.Error(t, err)
			if tc.wantErrIs != nil {
				require.ErrorIs(t, err, tc.wantErrIs)
			}
			requirePolicyFound(t, &f, f.account, false)
		})
	}
}

func TestChangeModeratorDelegatesToConstitutionAndCanonicalizesHex(t *testing.T) {
	f := newKeeperFixture(t, nil)
	newModeratorHex := testHexAddress(0x05)
	newModeratorCanonical := testCanonicalAddress(t, f.addressCodec, 0x05)

	_, err := NewMsgServer(&f.keeper).ChangeModerator(f.ctx, &types.MsgChangeModerator{
		ModeratorAddress:    testHexAddress(0x02),
		NewModeratorAddress: newModeratorHex,
	})
	require.NoError(t, err)
	require.Equal(t, []string{newModeratorCanonical}, f.constitutionKeeper.updates)
	moderator, err := f.keeper.GetModeratorAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, newModeratorCanonical, moderator)
}

func TestChangeModeratorConstitutionUpdateFailureHasNoEventOrStateMutation(t *testing.T) {
	f := newKeeperFixture(t, nil)
	server := NewMsgServer(&f.keeper)
	updateErr := errors.New("constitution update failed")
	f.constitutionKeeper.updateErr = updateErr
	newModeratorCanonical := testCanonicalAddress(t, f.addressCodec, 0x05)
	beforeEvents := len(f.ctx.EventManager().Events())

	_, err := server.ChangeModerator(f.ctx, &types.MsgChangeModerator{
		ModeratorAddress:    testHexAddress(0x02),
		NewModeratorAddress: testHexAddress(0x05),
	})
	require.ErrorIs(t, err, updateErr)
	require.Equal(t, f.moderator, f.constitutionKeeper.moderatorAddress)
	require.Equal(t, []string{newModeratorCanonical}, f.constitutionKeeper.updates)
	require.Len(t, f.ctx.EventManager().Events(), beforeEvents)
}

func TestMsgServerCRUDOverwriteGlobalRemoveAndEvents(t *testing.T) {
	f := newKeeperFixture(t, nil)
	server := NewMsgServer(&f.keeper)
	moderatorHex := testHexAddress(0x02)
	msgSend := &banktypes.MsgSend{}
	msgMultiSend := &banktypes.MsgMultiSend{}
	eventCount := len(f.ctx.EventManager().Events())

	_, err := server.RegisterDiscounts(f.ctx, &types.MsgRegisterDiscounts{
		ModeratorAddress: moderatorHex,
		Discounts: []types.AccountDiscount{
			testPolicy(f.accountHex, "bank", percentRule(msgSend, "25")),
			testPolicy("", "bank", fixedRule(msgSend, "4")),
		},
	})
	require.NoError(t, err)
	require.Len(t, f.ctx.EventManager().Events(), eventCount+1)
	require.Equal(t, types.EventTypeRegisterDiscounts, f.ctx.EventManager().Events()[eventCount].Type)
	requirePolicyFound(t, &f, f.account, true)
	requirePolicyFound(t, &f, "", true)

	// A second registration replaces the complete account record rather than
	// merging it with the prior bank module.
	_, err = server.RegisterDiscounts(f.ctx, &types.MsgRegisterDiscounts{
		ModeratorAddress: moderatorHex,
		Discounts: []types.AccountDiscount{{
			Address: f.accountHex,
			Modules: []types.ModuleDiscount{
				{Module: "first", Discounts: []types.Discount{percentRule(msgSend, "10"), percentRule(msgMultiSend, "20")}},
				{Module: "second", Discounts: []types.Discount{percentRule(msgSend, "30")}},
			},
		}},
	})
	require.NoError(t, err)
	stored := requirePolicyFound(t, &f, f.account, true)
	require.Len(t, stored.Modules, 2)
	require.NotEqual(t, "bank", stored.Modules[0].Module)

	// Compatibility: a non-empty msg_type ignores Module and removes its first
	// occurrence from every module.
	eventCount = len(f.ctx.EventManager().Events())
	_, err = server.RemoveDiscounts(f.ctx, &types.MsgRemoveDiscounts{
		ModeratorAddress: moderatorHex,
		Address:          f.accountHex,
		Module:           "ignored",
		MsgType:          sdk.MsgTypeURL(msgSend),
	})
	require.NoError(t, err)
	require.Len(t, f.ctx.EventManager().Events(), eventCount+1)
	require.Equal(t, types.EventTypeRemoveDiscounts, f.ctx.EventManager().Events()[eventCount].Type)
	stored = requirePolicyFound(t, &f, f.account, true)
	require.Len(t, stored.Modules[0].Discounts, 1)
	require.Empty(t, stored.Modules[1].Discounts)

	// Empty module removes the whole global record. The explicit global CLI
	// selector exposes this retained wire/API capability.
	_, err = server.RemoveDiscounts(f.ctx, &types.MsgRemoveDiscounts{
		ModeratorAddress: moderatorHex,
		Address:          "",
	})
	require.NoError(t, err)
	requirePolicyFound(t, &f, "", false)

	// Empty msg_type with a non-empty module removes that module only and keeps
	// the account record, including an empty module list if it was the last one.
	_, err = server.RemoveDiscounts(f.ctx, &types.MsgRemoveDiscounts{
		ModeratorAddress: moderatorHex,
		Address:          f.accountHex,
		Module:           "first",
	})
	require.NoError(t, err)
	stored = requirePolicyFound(t, &f, f.account, true)
	require.Len(t, stored.Modules, 1)
	require.Equal(t, "second", stored.Modules[0].Module)
}

func TestQueryCompatibilityBehavior(t *testing.T) {
	f := newKeeperFixture(t, nil)
	server := NewQueryServer(&f.keeper)
	msg := &banktypes.MsgSend{}
	f.constitutionKeeper.moderatorAddress = testHexAddress(0x02)
	moderator, err := server.ModeratorAddress(f.ctx, &types.QueryModeratorAddressRequest{})
	require.NoError(t, err)
	require.Equal(t, f.moderator, moderator.ModeratorAddress, "query must canonicalize Constitution's raw representation")
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy(f.account, "bank", percentRule(msg, "25"))))
	require.NoError(t, f.keeper.SetAccountDiscounts(f.ctx, testPolicy("", "bank", fixedRule(msg, "3"))))

	firstPage, err := server.Discounts(f.ctx, &types.QueryDiscountsRequest{
		Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Discounts, 1)
	require.Equal(t, uint64(2), firstPage.Pagination.Total)
	require.NotEmpty(t, firstPage.Pagination.NextKey)

	secondPage, err := server.Discounts(f.ctx, &types.QueryDiscountsRequest{
		Pagination: &query.PageRequest{Key: firstPage.Pagination.NextKey, Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Discounts, 1)
	require.Empty(t, secondPage.Pagination.NextKey)
	require.ElementsMatch(t,
		[]string{"", f.account},
		[]string{firstPage.Discounts[0].Address, secondPage.Discounts[0].Address},
	)
	_, err = server.Discounts(f.ctx, nil)
	require.Error(t, err)

	global, err := server.Discount(f.ctx, &types.QueryDiscountRequest{Address: ""})
	require.NoError(t, err)
	require.Empty(t, global.Discount.Address)
	require.Len(t, global.Discount.Modules, 1)
	account, err := server.Discount(f.ctx, &types.QueryDiscountRequest{Address: f.accountHex})
	require.NoError(t, err)
	require.Equal(t, f.account, account.Discount.Address)

	missing := testCanonicalAddress(t, f.addressCodec, 0x09)
	missingResponse, err := server.Discount(f.ctx, &types.QueryDiscountRequest{Address: missing})
	require.NoError(t, err)
	require.Equal(t, types.AccountDiscount{}, missingResponse.Discount)
}
