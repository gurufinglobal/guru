package bex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAppModuleBasicsAndServices(t *testing.T) {
	am, _ := setupAppModule(t)

	am.IsOnePerModuleType()
	am.IsAppModule()
	require.Equal(t, types.ModuleName, am.Name())
	require.Equal(t, uint64(ConsensusVersion), am.ConsensusVersion())
	am.RegisterLegacyAminoCodec(codec.NewLegacyAmino())

	registry := codectypes.NewInterfaceRegistry()
	am.RegisterInterfaces(registry)
	require.NoError(t, registry.EnsureRegistered(&bexv1.MsgRegisterAdmin{}))
	require.NoError(t, registry.EnsureRegistered(&bexv1.MsgRegisterAdminResponse{}))

	server := grpc.NewServer()
	require.NoError(t, am.RegisterServices(server))

	invariants := &mockInvariantRegistry{}
	am.RegisterInvariants(invariants)
	require.Len(t, invariants.routes, 1)
	require.Equal(t, types.ModuleName, invariants.routes[0].moduleName)

	am.RegisterGRPCGatewayRoutes(client.Context{}, gatewayruntime.NewServeMux())
	originalGateway := registerQueryGateway
	registerQueryGateway = func(context.Context, *gatewayruntime.ServeMux, bexv1.QueryClient) error {
		return errors.New("gateway failed")
	}
	t.Cleanup(func() { registerQueryGateway = originalGateway })
	require.Panics(t, func() {
		am.RegisterGRPCGatewayRoutes(client.Context{}, gatewayruntime.NewServeMux())
	})
}

type mockInvariantRegistry struct {
	routes []mockInvariantRoute
}

type mockInvariantRoute struct {
	moduleName string
	route      string
	invariant  sdk.Invariant
}

func (r *mockInvariantRegistry) RegisterRoute(moduleName, route string, invariant sdk.Invariant) {
	r.routes = append(r.routes, mockInvariantRoute{moduleName: moduleName, route: route, invariant: invariant})
}

func TestGenesisReadWriteValidateInitExport(t *testing.T) {
	am, ctx := setupAppModule(t)
	target := newMemoryGenesisTarget()
	require.NoError(t, am.DefaultGenesis(target.target))
	require.JSONEq(t, `1`, target.fields["next_exchange_id"].String())

	require.NoError(t, am.ValidateGenesis(target.source))
	require.NoError(t, am.InitGenesis(ctx, target.source))

	exported := newMemoryGenesisTarget()
	require.NoError(t, am.ExportGenesis(ctx, exported.target))
	require.JSONEq(t, `1`, exported.fields["next_exchange_id"].String())

	genesis, err := readGenesisState(sourceFrom(map[string]string{
		"admins":           `[]`,
		"exchanges":        `[]`,
		"collected_fees":   `[]`,
		"locked_fees":      `[]`,
		"volume_windows":   `[]`,
		"next_exchange_id": `7`,
	}), am.defaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, uint64(7), genesis.GetNextExchangeId())
	require.NoError(t, writeGenesisState(newMemoryGenesisTarget().target, genesis))

	fullGenesis := validGenesisState(t, am, ctx)
	require.NoError(t, am.validateGenesisState(ctx, fullGenesis))
	fullTarget := newMemoryGenesisTarget()
	require.NoError(t, writeGenesisState(fullTarget.target, fullGenesis))
	roundTrip, err := readGenesisState(fullTarget.source, am.defaultGenesisState())
	require.NoError(t, err)
	require.Len(t, roundTrip.GetAdmins(), 1)
	require.Len(t, roundTrip.GetExchanges(), 1)
	require.Len(t, roundTrip.GetCollectedFees(), 1)
	require.Len(t, roundTrip.GetLockedFees(), 1)
	require.Len(t, roundTrip.GetVolumeWindows(), 1)
}

func TestGenesisErrors(t *testing.T) {
	am, ctx := setupAppModule(t)
	readErr := errors.New("read failed")
	require.ErrorIs(t, am.ValidateGenesis(func(string) (io.ReadCloser, error) {
		return nil, readErr
	}), types.ErrReadGenesisField)
	require.ErrorIs(t, am.ValidateGenesis(sourceFrom(map[string]string{"admins": `[`})), types.ErrDecodeGenesisField)
	require.ErrorIs(t, am.InitGenesis(ctx, func(string) (io.ReadCloser, error) {
		return nil, readErr
	}), types.ErrReadGenesisField)
	require.ErrorIs(t, am.InitGenesis(ctx, sourceFrom(map[string]string{"next_exchange_id": `0`})), types.ErrInvalidGenesis)
	require.ErrorIs(t, am.DefaultGenesis(func(string) (io.WriteCloser, error) {
		return nil, errors.New("open failed")
	}), types.ErrOpenGenesisTargetField)
	require.ErrorIs(t, am.DefaultGenesis(func(string) (io.WriteCloser, error) {
		return nil, nil
	}), types.ErrNilGenesisTargetWriter)
	require.ErrorIs(t, writeGenesisState(func(string) (io.WriteCloser, error) {
		return errorWriteCloser{}, nil
	}, am.defaultGenesisState()), types.ErrEncodeGenesisField)
	require.ErrorIs(t, writeGenesisState(func(string) (io.WriteCloser, error) {
		return closeErrorWriteCloser{Buffer: &bytes.Buffer{}}, nil
	}, am.defaultGenesisState()), types.ErrCloseGenesisFieldWriter)
	_, err := readGenesisState(sourceFrom(map[string]string{"next_exchange_id": `bad`}), am.defaultGenesisState())
	require.ErrorIs(t, err, types.ErrDecodeGenesisField)
	require.ErrorIs(t, writeGenesisState(newMemoryGenesisTarget().target, nil), types.ErrInvalidGenesis)

	for _, field := range []string{"exchanges", "collected_fees", "locked_fees", "volume_windows"} {
		_, err := readGenesisState(sourceFrom(map[string]string{field: `bad`}), am.defaultGenesisState())
		require.ErrorIs(t, err, types.ErrDecodeGenesisField, field)
	}
	for _, field := range []string{"exchanges", "collected_fees", "locked_fees", "volume_windows", "next_exchange_id"} {
		require.ErrorIs(t, writeGenesisState(failOpenTarget(field), am.defaultGenesisState()), types.ErrOpenGenesisTargetField, field)
	}
	require.ErrorIs(t, am.ExportGenesis(ctx, failOpenTarget("admins")), types.ErrOpenGenesisTargetField)
	faultySetAM, faultySetCtx := faultyAppModule(t, "set", 0x01)
	require.ErrorIs(t, faultySetAM.InitGenesis(faultySetCtx, sourceFrom(map[string]string{"admins": `["` + rootAddressString(t, 0x21) + `"]`})), errStoreFault)
	faultyIteratorAM, faultyIteratorCtx := faultyAppModule(t, "iterator", 0x01)
	require.ErrorIs(t, faultyIteratorAM.ExportGenesis(faultyIteratorCtx, newMemoryGenesisTarget().target), errStoreFault)

	require.ErrorIs(t, am.validateGenesisState(ctx, nil), types.ErrInvalidGenesis)
	badCodecAM, badCodecCtx := setupAppModuleWithCodec(t, failingAddressCodec{base: evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr), failBytesToString: true})
	require.Error(t, badCodecAM.validateGenesisState(badCodecCtx, mutateGenesis(validGenesisState(t, am, ctx), func(g *bexv1.GenesisState) {
		g.Admins = nil
	})))
	requireGenesisInvalid(t, am, ctx, &bexv1.GenesisState{Admins: []string{"bad"}, NextExchangeId: 1})
	admin := rootAddressString(t, 0x22)
	requireGenesisInvalid(t, am, ctx, &bexv1.GenesisState{Admins: []string{admin, admin}, NextExchangeId: 1})

	valid := validGenesisState(t, am, ctx)
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.Exchanges[0].Id = 0 }))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.Exchanges = append(g.Exchanges, cloneExchange(g.Exchanges[0])) }))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.Exchanges[0].ReserveAddress = admin }))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.Exchanges[0].AdminAddress = "bad" }))
	require.Error(t, am.validateGenesisState(ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.Exchanges[0].DenomA = "bad denom" })))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.NextExchangeId = g.Exchanges[0].GetId() }))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.CollectedFees[0].ExchangeId = 999 }))
	require.Error(t, am.validateGenesisState(ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) {
		g.CollectedFees[0].Coins[0].Amount = "bad"
	})))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.LockedFees[0].ExchangeId = 999 }))
	require.Error(t, am.validateGenesisState(ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) {
		g.LockedFees[0].Coins[0].Amount = "bad"
	})))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.LockedFees[0].Coins[0].Amount = "11" }))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.VolumeWindows[0].ExchangeId = 999 }))
	requireGenesisInvalid(t, am, ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) {
		g.VolumeWindows[0].Direction = bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED
	}))
	require.Error(t, am.validateGenesisState(ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.VolumeWindows[0].EpochSeconds = 1 })))
	require.Error(t, am.validateGenesisState(ctx, mutateGenesis(valid, func(g *bexv1.GenesisState) { g.VolumeWindows[0].Amount = "bad" })))
}

func setupAppModule(t *testing.T) (AppModule, sdk.Context) {
	t.Helper()

	return setupAppModuleWithCodec(t, evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr))
}

func setupAppModuleWithCodec(t *testing.T, accountCodec address.Codec) (AppModule, sdk.Context) {
	t.Helper()

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_bex_module_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	keeper := bexkeeper.NewKeeper(
		runtime.NewKVStoreService(key),
		accountCodec,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return NewAppModule(keeper), testCtx.Ctx
}

func validGenesisState(t *testing.T, am AppModule, ctx sdk.Context) *bexv1.GenesisState {
	t.Helper()

	admin := rootAddressString(t, 0x11)
	reserve, err := am.keeper.GetReserveAddressString(ctx, 1)
	require.NoError(t, err)
	return &bexv1.GenesisState{
		Admins: []string{admin},
		Exchanges: []*bexv1.Exchange{{
			Id:                        1,
			AdminAddress:              admin,
			ReserveAddress:            reserve,
			DenomA:                    "agxn",
			PortA:                     "transwap",
			ChannelA:                  "channel-0",
			IbcDenomA:                 "ibc/a",
			DenomB:                    "gxusd",
			PortB:                     "transwap",
			ChannelB:                  "channel-1",
			IbcDenomB:                 "ibc/b",
			OracleSymbolAToB:          "AGXN/GXUSD",
			OracleSymbolBToA:          "GXUSD/AGXN",
			FeeBpsAToB:                25,
			FeeBpsBToA:                10,
			LimitAToB:                 "10000",
			LimitBToA:                 "10000",
			VolumeCapAToB:             "1000",
			VolumeCapBToA:             "1000",
			Revision:                  1,
			Status:                    bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
			Metadata:                  map[string]string{"venue": "bex-test"},
			VolumeEpochSeconds:        86400,
			MaxOracleStalenessSeconds: 300,
		}},
		CollectedFees: []*bexv1.FeeGenesis{{
			ExchangeId: 1,
			Coins:      []*basev1beta1.Coin{{Denom: "agxn", Amount: "10"}},
		}},
		LockedFees: []*bexv1.FeeGenesis{{
			ExchangeId: 1,
			Coins:      []*basev1beta1.Coin{{Denom: "agxn", Amount: "2"}},
		}},
		VolumeWindows: []*bexv1.VolumeWindowGenesis{{
			ExchangeId:     1,
			Direction:      bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
			EpochStartUnix: 1700000000,
			EpochSeconds:   86400,
			Amount:         "5",
		}},
		NextExchangeId: 2,
	}
}

func rootAddressString(t *testing.T, b byte) string {
	t.Helper()

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	address, err := accountCodec.BytesToString(sdk.AccAddress(bytes.Repeat([]byte{b}, 20)))
	require.NoError(t, err)
	return address
}

func requireGenesisInvalid(t *testing.T, am AppModule, ctx sdk.Context, genesis *bexv1.GenesisState) {
	t.Helper()

	require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
}

func mutateGenesis(genesis *bexv1.GenesisState, mutate func(*bexv1.GenesisState)) *bexv1.GenesisState {
	copied := &bexv1.GenesisState{
		Admins:         append([]string(nil), genesis.GetAdmins()...),
		Exchanges:      make([]*bexv1.Exchange, 0, len(genesis.GetExchanges())),
		CollectedFees:  make([]*bexv1.FeeGenesis, 0, len(genesis.GetCollectedFees())),
		LockedFees:     make([]*bexv1.FeeGenesis, 0, len(genesis.GetLockedFees())),
		VolumeWindows:  make([]*bexv1.VolumeWindowGenesis, 0, len(genesis.GetVolumeWindows())),
		NextExchangeId: genesis.GetNextExchangeId(),
	}
	for _, exchange := range genesis.GetExchanges() {
		copied.Exchanges = append(copied.Exchanges, cloneExchange(exchange))
	}
	for _, fee := range genesis.GetCollectedFees() {
		copied.CollectedFees = append(copied.CollectedFees, cloneFeeGenesis(fee))
	}
	for _, fee := range genesis.GetLockedFees() {
		copied.LockedFees = append(copied.LockedFees, cloneFeeGenesis(fee))
	}
	for _, window := range genesis.GetVolumeWindows() {
		windowCopy := *window
		copied.VolumeWindows = append(copied.VolumeWindows, &windowCopy)
	}
	mutate(copied)
	return copied
}

func cloneExchange(exchange *bexv1.Exchange) *bexv1.Exchange {
	copied := *exchange
	if exchange.GetMetadata() != nil {
		copied.Metadata = map[string]string{}
		for key, value := range exchange.GetMetadata() {
			copied.Metadata[key] = value
		}
	}
	return &copied
}

func cloneFeeGenesis(fee *bexv1.FeeGenesis) *bexv1.FeeGenesis {
	copied := &bexv1.FeeGenesis{ExchangeId: fee.GetExchangeId(), Coins: make([]*basev1beta1.Coin, 0, len(fee.GetCoins()))}
	for _, coin := range fee.GetCoins() {
		coinCopy := *coin
		copied.Coins = append(copied.Coins, &coinCopy)
	}
	return copied
}

func failOpenTarget(field string) appmodule.GenesisTarget {
	return func(name string) (io.WriteCloser, error) {
		if name == field {
			return nil, errors.New("open failed")
		}
		return nopWriteCloser{Buffer: &bytes.Buffer{}}, nil
	}
}

var errStoreFault = errors.New("store fault")

func faultyAppModule(t *testing.T, op string, prefix byte) (AppModule, sdk.Context) {
	t.Helper()

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_bex_module_fault_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	keeper := bexkeeper.NewKeeper(
		faultStoreService{base: runtime.NewKVStoreService(key), op: op, prefix: prefix},
		evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return NewAppModule(keeper), testCtx.Ctx
}

type faultStoreService struct {
	base   corestore.KVStoreService
	op     string
	prefix byte
}

func (s faultStoreService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return faultKVStore{KVStore: s.base.OpenKVStore(ctx), op: s.op, prefix: s.prefix}
}

type faultKVStore struct {
	corestore.KVStore
	op     string
	prefix byte
}

func (s faultKVStore) fail(op string, key []byte) error {
	if s.op != op {
		return nil
	}
	if s.prefix != 0 && (len(key) == 0 || key[0] != s.prefix) {
		return nil
	}
	return errStoreFault
}

func (s faultKVStore) Set(key, value []byte) error {
	if err := s.fail("set", key); err != nil {
		return err
	}
	return s.KVStore.Set(key, value)
}

func (s faultKVStore) Iterator(start, end []byte) (corestore.Iterator, error) {
	if err := s.fail("iterator", start); err != nil {
		return nil, err
	}
	return s.KVStore.Iterator(start, end)
}

type failingAddressCodec struct {
	base              address.Codec
	failBytesToString bool
}

func (c failingAddressCodec) StringToBytes(text string) ([]byte, error) {
	return c.base.StringToBytes(text)
}

func (c failingAddressCodec) BytesToString(bz []byte) (string, error) {
	if c.failBytesToString {
		return "", errors.New("bytes-to-string failed")
	}
	return c.base.BytesToString(bz)
}

type memoryGenesisTarget struct {
	fields map[string]*bytes.Buffer
}

func newMemoryGenesisTarget() *memoryGenesisTarget {
	return &memoryGenesisTarget{fields: map[string]*bytes.Buffer{}}
}

func (m *memoryGenesisTarget) target(field string) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	m.fields[field] = buf
	return nopWriteCloser{Buffer: buf}, nil
}

func (m *memoryGenesisTarget) source(field string) (io.ReadCloser, error) {
	buf, ok := m.fields[field]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func sourceFrom(fields map[string]string) appmodule.GenesisSource {
	return func(field string) (io.ReadCloser, error) {
		value, ok := fields[field]
		if !ok {
			return nil, nil
		}
		return io.NopCloser(strings.NewReader(value)), nil
	}
}

type nopWriteCloser struct {
	*bytes.Buffer
}

func (nopWriteCloser) Close() error {
	return nil
}

type errorWriteCloser struct{}

func (errorWriteCloser) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (errorWriteCloser) Close() error {
	return nil
}

type closeErrorWriteCloser struct {
	*bytes.Buffer
}

func (closeErrorWriteCloser) Close() error {
	return errors.New("close failed")
}
