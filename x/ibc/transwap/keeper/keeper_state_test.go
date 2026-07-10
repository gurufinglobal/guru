package keeper

import (
	"bytes"
	"testing"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"
	tmdb "github.com/cosmos/cosmos-db"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdkruntime "github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func setupKeeperStateTester(t *testing.T) (Keeper, sdk.Context, *refundAccountingBankKeeper, *refundAccountingICS4Wrapper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeDB, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, tmproto.Header{ChainID: "transwap-test"}, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewInfiniteGasMeter())

	bank := newRefundAccountingBankKeeper()
	ics4 := &refundAccountingICS4Wrapper{sequence: 88}

	return Keeper{
		storeService: sdkruntime.NewKVStoreService(storeKey),
		cdc:          cdc,
		AuthKeeper:   refundAccountingAccountKeeper{moduleAddr: authtypes.NewModuleAddress(types.ModuleName)},
		BankKeeper:   bank,
		ics4Wrapper:  ics4,
	}, ctx, bank, ics4
}

func TestKeeperPortAndDenomStoreRoundTrip(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)

	k.SetPort(ctx, "test-port")
	require.Equal(t, "test-port", k.GetPort(ctx))

	native := types.NewDenom("ugxusd")
	traced := types.NewDenom("ugxkrw", types.NewHop(types.PortID, "channel-7"))

	k.SetDenom(ctx, native)
	k.SetDenom(ctx, traced)

	denomHash := types.DenomHash(traced)
	got, found := k.GetDenom(ctx, denomHash)
	require.True(t, found)
	require.Equal(t, traced.Base, got.Base)
	require.True(t, k.HasDenom(ctx, denomHash))

	denoms := k.GetAllDenoms(ctx)
	require.Len(t, denoms, 2)
	require.Equal(t, types.DenomPath(traced), types.DenomPath(denoms[0]))
	require.Equal(t, types.DenomPath(native), types.DenomPath(denoms[1]))
}

func TestKeeperTotalEscrowedAndInvalidEntries(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)

	k.SetTotalEscrowForDenom(ctx, sdk.NewCoin("agxn", math.NewInt(10)))
	k.SetTotalEscrowForDenom(ctx, sdk.NewCoin("agxn", math.NewInt(7)))
	total := k.GetTotalEscrowForDenom(ctx, "agxn")
	require.Equal(t, math.NewInt(7), total.Amount)

	storeAdapter := sdkruntime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	storeAdapter.Set(types.TotalEscrowForDenomKey("bad"), []byte{0x00})

	all := k.GetAllTotalEscrowed(ctx)
	require.Len(t, all, 1)
	require.Equal(t, math.NewInt(7), all[0].Amount)

	exported := k.ExportGenesis(ctx)
	require.Len(t, exported.TotalEscrowed, 1)
	require.Equal(t, "agxn", exported.TotalEscrowed[0].Denom)
	require.Equal(t, "7", exported.TotalEscrowed[0].Amount)

	k.SetTotalEscrowForDenom(ctx, sdk.NewCoin("agxn", math.ZeroInt()))
	all = k.GetAllTotalEscrowed(ctx)
	require.Len(t, all, 0)

	exported = k.ExportGenesis(ctx)
	require.Empty(t, exported.TotalEscrowed)
}

func TestTokenFromCoinAndIBCDenomResolution(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)

	native := sdk.NewCoin("uatom", math.NewInt(123))
	token, err := k.TokenFromCoin(ctx, native)
	require.NoError(t, err)
	require.Equal(t, "uatom", token.Denom.Base)
	require.Equal(t, "123", token.Amount)

	traced := types.NewDenom("ugxusd", types.NewHop(types.PortID, "channel-0"))
	k.SetDenom(ctx, traced)
	ibcDenom := types.DenomIBCDenom(traced)

	ibcToken, err := k.TokenFromCoin(ctx, sdk.NewCoin(ibcDenom, math.NewInt(9)))
	require.NoError(t, err)
	require.Equal(t, "ugxusd", ibcToken.Denom.Base)

	nativeToken, err := k.TokenFromCoin(ctx, sdk.NewCoin("bad", math.NewInt(1)))
	require.NoError(t, err)
	require.Equal(t, "bad", nativeToken.Denom.Base)

	missingDenom := types.DenomIBCDenom(types.NewDenom("missing", types.NewHop(types.PortID, "channel-0")))
	_, err = k.TokenFromCoin(ctx, sdk.NewCoin(missingDenom, math.NewInt(1)))
	require.Error(t, err)
}

func TestGetDenomFromIBCDenomAndDenomPathFromHash(t *testing.T) {
	k, ctx, _, _ := setupKeeperStateTester(t)

	denom := types.NewDenom("ugxusd", types.NewHop(types.PortID, "channel-0"))
	k.SetDenom(ctx, denom)
	ibcDenom := types.DenomIBCDenom(denom)

	resolved, err := k.GetDenomFromIBCDenom(ctx, ibcDenom)
	require.NoError(t, err)
	require.Equal(t, denom.Base, resolved.Base)

	trace, err := k.DenomPathFromHash(ctx, ibcDenom)
	require.NoError(t, err)
	require.Equal(t, types.DenomPath(denom), trace)

	_, err = k.GetDenomFromIBCDenom(ctx, "not-a-valid-hex")
	require.Error(t, err)

	_, err = k.GetDenomFromIBCDenom(ctx, "ibc/1234")
	require.Error(t, err)
}

func TestEscrowAndUnescrowCoin(t *testing.T) {
	k, ctx, bank, _ := setupKeeperStateTester(t)

	sender := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	escrow := types.GetEscrowAddress(types.PortID, "channel-0")
	denom := "axlusdc"

	bank.SetBalance(sender, sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(10))))
	require.NoError(t, k.EscrowCoin(ctx, sender, escrow, sdk.NewCoin(denom, math.NewInt(4))))
	require.Equal(t, math.NewInt(6), bank.GetAllBalances(ctx, sender).AmountOf(denom))
	require.Equal(t, math.NewInt(4), bank.GetAllBalances(ctx, escrow).AmountOf(denom))
	require.Equal(t, math.NewInt(4), k.GetTotalEscrowForDenom(ctx, denom).Amount)

	require.NoError(t, k.UnescrowCoin(ctx, escrow, receiver, sdk.NewCoin(denom, math.NewInt(4))))
	require.Equal(t, math.NewInt(0), bank.GetAllBalances(ctx, escrow).AmountOf(denom))
	require.Equal(t, math.NewInt(4), bank.GetAllBalances(ctx, receiver).AmountOf(denom))
	require.True(t, k.GetTotalEscrowForDenom(ctx, denom).Amount.IsZero())
}

func TestSendTransferAndTransferV1Packet(t *testing.T) {
	t.Run("source channel send burns prefixed tokens", func(t *testing.T) {
		k, ctx, bank, _ := setupKeeperStateTester(t)

		sender := sdk.AccAddress(bytes.Repeat([]byte{0x31}, 20))
		prefixedDenom := types.DenomIBCDenom(types.NewDenom("uatom", types.NewHop(types.PortID, "channel-0")))
		bank.SetBalance(sender, sdk.NewCoins(sdk.NewCoin(prefixedDenom, math.NewInt(10))))

		token := transwapv1.Token{
			Denom:  types.NewDenom("uatom", types.NewHop(types.PortID, "channel-0")),
			Amount: "2",
		}
		require.NoError(t, k.SendTransfer(ctx, types.PortID, "channel-0", token, sender))

		require.Equal(t, math.NewInt(8), bank.GetAllBalances(ctx, sender).AmountOf(prefixedDenom))
		require.True(t, bank.GetAllBalances(ctx, k.AuthKeeper.GetModuleAddress(types.ModuleName)).IsZero())
	})

	t.Run("sink channel send escrowes native token", func(t *testing.T) {
		k, ctx, bank, _ := setupKeeperStateTester(t)

		sender := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
		bank.SetBalance(sender, sdk.NewCoins(sdk.NewCoin("ubtc", math.NewInt(10))))
		escrow := types.GetEscrowAddress(types.PortID, "channel-0")

		token := transwapv1.Token{
			Denom:  types.NewDenom("ubtc"),
			Amount: "2",
		}
		require.NoError(t, k.SendTransfer(ctx, types.PortID, "channel-0", token, sender))

		require.Equal(t, math.NewInt(8), bank.GetAllBalances(ctx, sender).AmountOf("ubtc"))
		require.Equal(t, math.NewInt(2), bank.GetAllBalances(ctx, escrow).AmountOf("ubtc"))
		require.Equal(t, math.NewInt(2), k.GetTotalEscrowForDenom(ctx, "ubtc").Amount)
	})

	t.Run("transferV1Packet sends packet and returns sequence", func(t *testing.T) {
		k, ctx, bank, ics4 := setupKeeperStateTester(t)
		sender := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
		receiver := sdk.AccAddress(bytes.Repeat([]byte{0x45}, 20))
		bank.SetBalance(sender, sdk.NewCoins(sdk.NewCoin("uexp", math.NewInt(10))))

		data := types.NewFungibleTokenPacketData("uexp", "2", sender.String(), receiver.String(), "memo")
		token := transwapv1.Token{Denom: types.NewDenom("uexp"), Amount: "2"}
		sequence, err := k.transferV1Packet(ctx, "channel-0", token, 1000, data)
		require.NoError(t, err)
		require.Equal(t, uint64(88), sequence)

		require.Equal(t, math.NewInt(8), bank.GetAllBalances(ctx, sender).AmountOf("uexp"))
		require.Equal(t, uint64(88), ics4.sequence)
	})

	t.Run("transferV1Packet panics on invalid sender bech32", func(t *testing.T) {
		k, ctx, _, _ := setupKeeperStateTester(t)
		badPacket := types.NewFungibleTokenPacketData("uexp", "2", "bad-sender", "receiver", "")
		require.Panics(t, func() {
			_, _ = k.transferV1Packet(ctx, "channel-0", transwapv1.Token{
				Denom:  types.NewDenom("uexp"),
				Amount: "2",
			}, 0, badPacket)
		})
	})
}

func TestRefundPacketTokensUsesMintOrUnescrowPath(t *testing.T) {
	k, ctx, bank, _ := setupKeeperStateTester(t)

	reserve := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
	escrow := types.GetEscrowAddress(types.PortID, "channel-0")

	mintPathToken := transwapv1.Token{
		Denom:  types.NewDenom("uatom", types.NewHop(types.PortID, "channel-0")),
		Amount: "5",
	}
	mintData := types.NewInternalTransferRepresentation("0", mintPathToken, reserve.String(), "target", "")
	require.NoError(t, k.refundPacketTokens(ctx, types.PortID, "channel-0", mintData))
	require.Equal(t, math.NewInt(5), bank.GetAllBalances(ctx, reserve).AmountOf(types.DenomIBCDenom(mintPathToken.Denom)))

	unescrowToken := transwapv1.Token{
		Denom:  types.NewDenom("uatom"),
		Amount: "7",
	}
	sender := sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x78}, 20))
	// place tokens into escrow and total escrow snapshot before refunding
	bank.SetBalance(receiver, sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(7))))
	require.NoError(t, k.EscrowCoin(ctx, receiver, escrow, sdk.NewCoin("uatom", math.NewInt(7))))

	unescrowData := types.NewInternalTransferRepresentation("0", unescrowToken, sender.String(), "target", "")
	require.NoError(t, k.refundPacketTokens(ctx, types.PortID, "channel-0", unescrowData))
	require.Equal(t, math.NewInt(7), bank.GetAllBalances(ctx, sender).AmountOf("uatom"))
	require.True(t, k.GetTotalEscrowForDenom(ctx, unescrowToken.Denom.GetBase()).Amount.IsZero())
}
