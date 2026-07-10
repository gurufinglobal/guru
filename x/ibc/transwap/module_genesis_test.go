package transwap

import (
	"bytes"
	"errors"
	"io"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestAppModuleGenesisRoundTrip(t *testing.T) {
	k, ctx, _, _, _ := setupIBCModuleAckRefund(t)
	am := NewAppModule(k)

	defaults := newTranswapGenesisFields()
	require.NoError(t, am.DefaultGenesis(defaults.target))
	require.NoError(t, am.ValidateGenesis(defaults.source))
	defaultState, err := readGenesisState(defaults.source, types.DefaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, types.PortID, defaultState.GetPortId())
	require.Empty(t, defaultState.GetDenoms())
	require.Empty(t, defaultState.GetTotalEscrowed())

	state := types.NewGenesisState(
		types.PortID,
		types.Denoms{
			types.NewDenom("uatom"),
			types.NewDenom("uusdc", types.NewHop(types.PortID, "channel-7")),
		},
		sdk.NewCoins(sdk.NewInt64Coin("uatom", 25)),
	)
	input := newTranswapGenesisFields()
	require.NoError(t, writeGenesisState(input.target, state))
	require.NoError(t, am.ValidateGenesis(input.source))
	require.NoError(t, am.InitGenesis(ctx, input.source))

	exported := newTranswapGenesisFields()
	require.NoError(t, am.ExportGenesis(ctx, exported.target))
	got, err := readGenesisState(exported.source, types.DefaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, state.GetPortId(), got.GetPortId())
	require.Equal(t, state.GetDenoms(), got.GetDenoms())

	gotEscrowed, err := types.ProtoCoinsToSDK(got.GetTotalEscrowed())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uatom", 25)), gotEscrowed)
}

func TestAppModuleGenesisValidationRejectsInvalidStateBeforeWrite(t *testing.T) {
	tests := []struct {
		name  string
		state *transwapv1.GenesisState
	}{
		{
			name:  "invalid port",
			state: types.NewGenesisState(" ", nil, nil),
		},
		{
			name: "duplicate denom",
			state: types.NewGenesisState(types.PortID, types.Denoms{
				types.NewDenom("uatom"),
				types.NewDenom("uatom"),
			}, nil),
		},
		{
			name: "invalid total escrow denom",
			state: &transwapv1.GenesisState{
				PortId: types.PortID,
				TotalEscrowed: []*basev1beta1.Coin{{
					Denom:  "bad denom",
					Amount: "1",
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := newTranswapGenesisFields()
			require.NoError(t, writeGenesisState(fields.target, tt.state))
			require.Error(t, (AppModule{}).ValidateGenesis(fields.source))
		})
	}

	k, ctx, _, _, _ := setupIBCModuleAckRefund(t)
	am := NewAppModule(k)
	invalid := newTranswapGenesisFields()
	require.NoError(t, writeGenesisState(invalid.target, tests[1].state))
	require.Error(t, am.InitGenesis(ctx, invalid.source))
	require.Empty(t, k.GetPort(ctx))
	require.Empty(t, k.GetAllDenoms(ctx))
}

func TestAppModuleGenesisIOErrorsAreClassified(t *testing.T) {
	am := AppModule{}
	readErr := errors.New("read failed")
	err := am.ValidateGenesis(func(string) (io.ReadCloser, error) {
		return nil, readErr
	})
	require.ErrorIs(t, err, types.ErrReadGenesisField)

	malformed := newTranswapGenesisFields()
	malformed.fields["port_id"] = []byte(`{"unterminated"`)
	err = am.ValidateGenesis(malformed.source)
	require.ErrorIs(t, err, types.ErrDecodeGenesisField)

	err = am.DefaultGenesis(func(string) (io.WriteCloser, error) {
		return nil, nil
	})
	require.ErrorIs(t, err, types.ErrNilGenesisTargetWriter)
}

type transwapGenesisFields struct {
	fields map[string][]byte
}

func newTranswapGenesisFields() *transwapGenesisFields {
	return &transwapGenesisFields{fields: make(map[string][]byte)}
}

func (m *transwapGenesisFields) source(field string) (io.ReadCloser, error) {
	value, ok := m.fields[field]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (m *transwapGenesisFields) target(field string) (io.WriteCloser, error) {
	return &transwapGenesisFieldWriter{field: field, fields: m.fields}, nil
}

type transwapGenesisFieldWriter struct {
	bytes.Buffer
	field  string
	fields map[string][]byte
}

func (w *transwapGenesisFieldWriter) Close() error {
	w.fields[w.field] = append([]byte(nil), w.Bytes()...)
	return nil
}
