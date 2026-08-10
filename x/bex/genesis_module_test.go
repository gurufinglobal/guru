package bex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"cosmossdk.io/core/appmodule"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bexkeeper "github.com/gurufinglobal/guru/v3/x/bex/keeper"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAppModuleServicesAndGateway(t *testing.T) {
	am, _ := setupAppModule(t)

	require.Equal(t, types.ModuleName, am.Name())
	require.Equal(t, uint64(ConsensusVersion), am.ConsensusVersion())

	registry := codectypes.NewInterfaceRegistry()
	am.RegisterInterfaces(registry)
	require.NoError(t, registry.EnsureRegistered(&types.MsgRegisterAdmin{}))
	require.NoError(t, registry.EnsureRegistered(&types.MsgRegisterAdminResponse{}))

	server := grpc.NewServer()
	require.NoError(t, am.RegisterServices(server))

	am.RegisterGRPCGatewayRoutes(client.Context{}, gatewayruntime.NewServeMux())
	originalGateway := registerQueryGateway
	registerQueryGateway = func(context.Context, *gatewayruntime.ServeMux, types.QueryClient) error {
		return errors.New("gateway failed")
	}
	t.Cleanup(func() { registerQueryGateway = originalGateway })
	require.Panics(t, func() {
		am.RegisterGRPCGatewayRoutes(client.Context{}, gatewayruntime.NewServeMux())
	})
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
		"admins":             `[]`,
		"exchanges":          `[]`,
		"collected_fees":     `[]`,
		"locked_fees":        `[]`,
		"volume_windows":     `[]`,
		"reserve_depositors": `[]`,
		"next_exchange_id":   `7`,
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
	require.Len(t, roundTrip.GetReserveDepositors(), 1)
}

func TestGenesisParsingAndIOErrors(t *testing.T) {
	am, _ := setupAppModule(t)
	readErr := errors.New("read failed")

	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{
			name: "reader open",
			run: func() error {
				return am.ValidateGenesis(func(string) (io.ReadCloser, error) { return nil, readErr })
			},
			want: types.ErrReadGenesisField,
		},
		{
			name: "reader close",
			run: func() error {
				_, err := readGenesisState(func(string) (io.ReadCloser, error) {
					return closeErrorReadCloser{Reader: strings.NewReader(`[]`)}, nil
				}, am.defaultGenesisState())
				return err
			},
			want: types.ErrReadGenesisField,
		},
		{
			name: "malformed JSON",
			run: func() error {
				return am.ValidateGenesis(sourceFrom(map[string]string{"admins": `[`}))
			},
			want: types.ErrDecodeGenesisField,
		},
		{
			name: "trailing JSON",
			run: func() error {
				return am.ValidateGenesis(sourceFrom(map[string]string{"admins": `[] []`}))
			},
			want: types.ErrDecodeGenesisField,
		},
		{
			name: "unknown field",
			run: func() error {
				return am.ValidateGenesis(sourceFrom(map[string]string{"exchanges": `[{"unknown_field":1}]`}))
			},
			want: types.ErrDecodeGenesisField,
		},
		{
			name: "writer open",
			run:  func() error { return am.DefaultGenesis(failOpenTarget("admins")) },
			want: types.ErrOpenGenesisTargetField,
		},
		{
			name: "nil writer",
			run: func() error {
				return am.DefaultGenesis(func(string) (io.WriteCloser, error) { return nil, nil })
			},
			want: types.ErrNilGenesisTargetWriter,
		},
		{
			name: "writer encode",
			run: func() error {
				return writeGenesisState(func(string) (io.WriteCloser, error) {
					return errorWriteCloser{}, nil
				}, am.defaultGenesisState())
			},
			want: types.ErrEncodeGenesisField,
		},
		{
			name: "writer close",
			run: func() error {
				return writeGenesisState(func(string) (io.WriteCloser, error) {
					return closeErrorWriteCloser{Buffer: &bytes.Buffer{}}, nil
				}, am.defaultGenesisState())
			},
			want: types.ErrCloseGenesisFieldWriter,
		},
		{
			name: "nil state",
			run:  func() error { return writeGenesisState(newMemoryGenesisTarget().target, nil) },
			want: types.ErrInvalidGenesis,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.run(), tc.want)
		})
	}
}

func TestGenesisSemanticValidation(t *testing.T) {
	am, ctx := setupAppModule(t)
	valid := validGenesisState(t, am, ctx)
	admin := rootAddressString(t, 0x22)
	secondReserve, err := am.keeper.GetReserveAddressString(ctx, 2)
	require.NoError(t, err)

	t.Run("valid tombstone and uint64 boundaries", func(t *testing.T) {
		tombstone := mutateGenesis(valid, func(g *types.GenesisState) {
			g.Exchanges[0].Status = types.ExchangeStatus_EXCHANGE_STATUS_DELETED
			g.CollectedFees = nil
			g.LockedFees = nil
		})
		require.NoError(t, am.validateGenesisState(ctx, tombstone))
		require.NoError(t, am.validateGenesisState(ctx, mutateGenesis(valid, func(g *types.GenesisState) {
			g.Exchanges[0].Revision = ^uint64(0)
			g.NextExchangeId = ^uint64(0)
		})))
	})

	tests := []struct {
		name         string
		mutate       func(*types.GenesisState)
		wantAnyError bool
	}{
		{name: "invalid admin", mutate: func(g *types.GenesisState) { g.Admins = []string{"bad"} }},
		{name: "duplicate admin", mutate: func(g *types.GenesisState) { g.Admins = []string{admin, admin} }},
		{name: "noncanonical admin", mutate: func(g *types.GenesisState) { g.Admins = []string{" " + admin} }},
		{name: "zero exchange id", mutate: func(g *types.GenesisState) { g.Exchanges[0].Id = 0 }},
		{name: "zero revision", mutate: func(g *types.GenesisState) { g.Exchanges[0].Revision = 0 }},
		{name: "zero volume generation", mutate: func(g *types.GenesisState) { g.Exchanges[0].VolumeWindowGeneration = 0 }},
		{name: "duplicate exchange id", mutate: func(g *types.GenesisState) { g.Exchanges = append(g.Exchanges, cloneExchange(g.Exchanges[0])) }},
		{name: "reserve address mismatch", mutate: func(g *types.GenesisState) { g.Exchanges[0].ReserveAddress = admin }},
		{name: "invalid exchange admin", mutate: func(g *types.GenesisState) { g.Exchanges[0].AdminAddress = "bad" }},
		{name: "noncanonical exchange admin", mutate: func(g *types.GenesisState) { g.Exchanges[0].AdminAddress = " " + g.Exchanges[0].AdminAddress }},
		{name: "IBC denom mismatch", mutate: func(g *types.GenesisState) { g.Exchanges[0].IbcDenomA = "ibc/WRONG" }},
		{name: "noncanonical route", mutate: func(g *types.GenesisState) { g.Exchanges[0].DenomA = " agxn" }},
		{name: "invalid route denom", mutate: func(g *types.GenesisState) { g.Exchanges[0].DenomA = "bad denom" }, wantAnyError: true},
		{name: "next id not above maximum", mutate: func(g *types.GenesisState) { g.NextExchangeId = g.Exchanges[0].GetId() }},
		{name: "collected fees unknown exchange", mutate: func(g *types.GenesisState) { g.CollectedFees[0].ExchangeId = 999 }},
		{name: "malformed collected amount", mutate: func(g *types.GenesisState) { g.CollectedFees[0].Coins[0].Amount = sdkmath.Int{} }, wantAnyError: true},
		{name: "unsupported collected denom", mutate: func(g *types.GenesisState) { g.CollectedFees[0].Coins[0].Denom = "unsupported" }},
		{name: "duplicate collected ledger", mutate: func(g *types.GenesisState) {
			g.CollectedFees = append(g.CollectedFees, cloneFeeGenesis(g.CollectedFees[0]))
		}},
		{name: "locked fees unknown exchange", mutate: func(g *types.GenesisState) { g.LockedFees[0].ExchangeId = 999 }},
		{name: "malformed locked amount", mutate: func(g *types.GenesisState) { g.LockedFees[0].Coins[0].Amount = sdkmath.Int{} }, wantAnyError: true},
		{name: "locked fees exceed collected", mutate: func(g *types.GenesisState) { g.LockedFees[0].Coins[0].Amount = sdkmath.NewInt(11) }},
		{name: "duplicate locked ledger", mutate: func(g *types.GenesisState) { g.LockedFees = append(g.LockedFees, cloneFeeGenesis(g.LockedFees[0])) }},
		{name: "deleted exchange has collected fees", mutate: func(g *types.GenesisState) {
			g.Exchanges[0].Status = types.ExchangeStatus_EXCHANGE_STATUS_DELETED
			g.LockedFees = nil
		}},
		{name: "deleted exchange has locked fees", mutate: func(g *types.GenesisState) {
			g.Exchanges[0].Status = types.ExchangeStatus_EXCHANGE_STATUS_DELETED
			g.CollectedFees = nil
		}},
		{name: "volume unknown exchange", mutate: func(g *types.GenesisState) { g.VolumeWindows[0].ExchangeId = 999 }},
		{name: "volume invalid direction", mutate: func(g *types.GenesisState) {
			g.VolumeWindows[0].Direction = types.SwapDirection_SWAP_DIRECTION_UNSPECIFIED
		}},
		{name: "volume invalid epoch", mutate: func(g *types.GenesisState) { g.VolumeWindows[0].EpochSeconds = 1 }, wantAnyError: true},
		{name: "volume unaligned epoch", mutate: func(g *types.GenesisState) { g.VolumeWindows[0].EpochStartUnix++ }},
		{name: "volume epoch overflow", mutate: func(g *types.GenesisState) {
			seconds := uint64(g.VolumeWindows[0].GetEpochSeconds())
			g.VolumeWindows[0].EpochStartUnix = ^uint64(0) / seconds * seconds
		}},
		{name: "malformed volume amount", mutate: func(g *types.GenesisState) { g.VolumeWindows[0].Amount = "bad" }, wantAnyError: true},
		{name: "depositor unknown exchange", mutate: func(g *types.GenesisState) { g.ReserveDepositors[0].ExchangeId = 999 }},
		{name: "invalid depositor address", mutate: func(g *types.GenesisState) { g.ReserveDepositors[0].DepositorAddress = "bad" }},
		{name: "noncanonical depositor", mutate: func(g *types.GenesisState) {
			g.ReserveDepositors[0].DepositorAddress = " " + g.ReserveDepositors[0].DepositorAddress
		}},
		{name: "duplicate depositor", mutate: func(g *types.GenesisState) { g.ReserveDepositors = append(g.ReserveDepositors, g.ReserveDepositors[0]) }},
		{
			name: "aggregate collected fee overflow",
			mutate: func(g *types.GenesisState) {
				second := cloneExchange(g.Exchanges[0])
				second.Id = 2
				second.ReserveAddress = secondReserve
				g.Exchanges = append(g.Exchanges, second)
				maxAmount, ok := sdkmath.NewIntFromString("115792089237316195423570985008687907853269984665640564039457584007913129639935")
				require.True(t, ok)
				g.CollectedFees[0].Coins[0].Amount = maxAmount
				g.CollectedFees = append(g.CollectedFees, &types.FeeGenesis{ExchangeId: 2, Coins: sdk.NewCoins(sdk.NewInt64Coin("agxn", 1))})
				g.NextExchangeId = 3
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := am.validateGenesisState(ctx, mutateGenesis(valid, tc.mutate))
			if tc.wantAnyError {
				require.Error(t, err)
				return
			}
			require.ErrorIs(t, err, types.ErrInvalidGenesis)
		})
	}

	t.Run("invalid fee denom returns an error without panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			genesis := mutateGenesis(valid, func(g *types.GenesisState) {
				g.CollectedFees[0].Coins[0].Denom = "bad denom"
			})
			require.Error(t, am.validateGenesisState(ctx, genesis))
		})
	})
}

func setupAppModule(t *testing.T) (AppModule, sdk.Context) {
	t.Helper()

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_bex_module_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)
	keeper := bexkeeper.NewKeeper(
		runtime.NewKVStoreService(key),
		codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
		evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return NewAppModule(keeper), testCtx.Ctx
}

func validGenesisState(t *testing.T, am AppModule, ctx sdk.Context) *types.GenesisState {
	t.Helper()

	admin := rootAddressString(t, 0x11)
	depositor := rootAddressString(t, 0x12)
	reserve, err := am.keeper.GetReserveAddressString(ctx, 1)
	require.NoError(t, err)
	ibcDenomA, err := bexkeeper.ExpectedIBCDenomForGenesis("agxn", "transwap", "channel-0")
	require.NoError(t, err)
	ibcDenomB, err := bexkeeper.ExpectedIBCDenomForGenesis("gxusd", "transwap", "channel-1")
	require.NoError(t, err)
	return &types.GenesisState{
		Admins: []string{admin},
		Exchanges: []*types.Exchange{{
			Id:                        1,
			AdminAddress:              admin,
			ReserveAddress:            reserve,
			DenomA:                    "agxn",
			PortA:                     "transwap",
			ChannelA:                  "channel-0",
			IbcDenomA:                 ibcDenomA,
			DenomB:                    "gxusd",
			PortB:                     "transwap",
			ChannelB:                  "channel-1",
			IbcDenomB:                 ibcDenomB,
			OracleSymbolAToB:          "AGXN/GXUSD",
			OracleSymbolBToA:          "GXUSD/AGXN",
			FeeBpsAToB:                25,
			FeeBpsBToA:                10,
			LimitAToB:                 "10000",
			LimitBToA:                 "10000",
			VolumeCapAToB:             "1000",
			VolumeCapBToA:             "1000",
			Revision:                  1,
			VolumeWindowGeneration:    1,
			Status:                    types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
			Metadata:                  map[string]string{"venue": "bex-test"},
			VolumeEpochSeconds:        86400,
			MaxOracleStalenessSeconds: 300,
		}},
		CollectedFees: []*types.FeeGenesis{{
			ExchangeId: 1,
			Coins:      sdk.NewCoins(sdk.NewInt64Coin("agxn", 10)),
		}},
		LockedFees: []*types.FeeGenesis{{
			ExchangeId: 1,
			Coins:      sdk.NewCoins(sdk.NewInt64Coin("agxn", 2)),
		}},
		VolumeWindows: []*types.VolumeWindowGenesis{{
			ExchangeId:             1,
			Direction:              types.SwapDirection_SWAP_DIRECTION_A_TO_B,
			EpochStartUnix:         1699920000,
			EpochSeconds:           86400,
			Amount:                 "5",
			VolumeWindowGeneration: 1,
		}},
		ReserveDepositors: []*types.ReserveDepositorGenesis{{
			ExchangeId:       1,
			DepositorAddress: depositor,
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

func requireGenesisInvalid(t *testing.T, am AppModule, ctx sdk.Context, genesis *types.GenesisState) {
	t.Helper()

	require.ErrorIs(t, am.validateGenesisState(ctx, genesis), types.ErrInvalidGenesis)
}

func mutateGenesis(genesis *types.GenesisState, mutate func(*types.GenesisState)) *types.GenesisState {
	copied := &types.GenesisState{
		Admins:            append([]string(nil), genesis.GetAdmins()...),
		Exchanges:         make([]*types.Exchange, 0, len(genesis.GetExchanges())),
		CollectedFees:     make([]*types.FeeGenesis, 0, len(genesis.GetCollectedFees())),
		LockedFees:        make([]*types.FeeGenesis, 0, len(genesis.GetLockedFees())),
		VolumeWindows:     make([]*types.VolumeWindowGenesis, 0, len(genesis.GetVolumeWindows())),
		ReserveDepositors: make([]*types.ReserveDepositorGenesis, 0, len(genesis.GetReserveDepositors())),
		NextExchangeId:    genesis.GetNextExchangeId(),
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
		copied.VolumeWindows = append(copied.VolumeWindows, types.CloneMessage(window))
	}
	for _, depositor := range genesis.GetReserveDepositors() {
		copied.ReserveDepositors = append(copied.ReserveDepositors, types.CloneMessage(depositor))
	}
	mutate(copied)
	return copied
}

func cloneExchange(exchange *types.Exchange) *types.Exchange {
	return types.CloneMessage(exchange)
}

func cloneFeeGenesis(fee *types.FeeGenesis) *types.FeeGenesis {
	if fee == nil {
		return nil
	}
	return &types.FeeGenesis{
		ExchangeId: fee.GetExchangeId(),
		Coins:      append(sdk.Coins(nil), fee.GetCoins()...),
	}
}

func failOpenTarget(field string) appmodule.GenesisTarget {
	return func(name string) (io.WriteCloser, error) {
		if name == field {
			return nil, errors.New("open failed")
		}
		return nopWriteCloser{Buffer: &bytes.Buffer{}}, nil
	}
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

type closeErrorReadCloser struct {
	io.Reader
}

func (closeErrorReadCloser) Close() error {
	return errors.New("close failed")
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
