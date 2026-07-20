package constitution

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutionkeeper "github.com/gurufinglobal/guru/v3/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"github.com/stretchr/testify/require"
)

type genesisTestFixture struct {
	ctx              sdk.Context
	module           AppModule
	keeper           constitutionkeeper.Keeper
	authorityAddress string
	baseAddress      string
	moderatorAddress string
}

func setupGenesisTestFixture(t *testing.T) genesisTestFixture {
	t.Helper()

	key := storetypes.NewKVStoreKey(constitutiontypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_constitution_genesis_test")
	testCtx := testutil.DefaultContextWithDB(t, key, transientKey)

	accountCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	authorityBytes := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	keeper := constitutionkeeper.NewKeeper(authorityBytes, runtime.NewKVStoreService(key), codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), accountCodec, nil)
	module := NewAppModule(keeper)

	authorityAddress, err := keeper.AuthorityAddressString()
	require.NoError(t, err)

	return genesisTestFixture{
		ctx:              testCtx.Ctx,
		module:           module,
		keeper:           keeper,
		authorityAddress: authorityAddress,
		baseAddress:      testGenesisAddress(t, accountCodec, 0x02),
		moderatorAddress: testGenesisAddress(t, accountCodec, 0x03),
	}
}

func testGenesisAddress(t *testing.T, accountCodec address.Codec, b byte) string {
	t.Helper()

	address, err := accountCodec.BytesToString(bytes.Repeat([]byte{b}, 20))
	require.NoError(t, err)
	return address
}

func validGenesisState(f genesisTestFixture) *constitutiontypes.GenesisState {
	return &constitutiontypes.GenesisState{
		Params: &constitutiontypes.Params{
			MinValidatorBondAmount: testGenesisMinBond(10),
		},
		BaseAddress:      f.baseAddress,
		ModeratorAddress: f.moderatorAddress,
		SeparationRatio: &constitutiontypes.SeparationRatio{
			BasePpm:       200_000,
			BurnPpm:       300_000,
			ValidatorsPpm: 500_000,
		},
	}
}

func testGenesisMinBond(amount int64) *sdk.Coin {
	coin := sdk.NewInt64Coin(appparams.BaseDenom, amount)
	return &coin
}

func validPendingMinGasPriceSchedule() *constitutiontypes.MinGasPriceSchedule {
	return &constitutiontypes.MinGasPriceSchedule{
		EffectiveHeight:                15,
		ScheduledMinGasPrice:           "630000000000.000000000000000000",
		SourceSymbol:                   appparams.MinGasPriceOracleSymbol,
		SourceValue:                    "1.0",
		SourceOracleHeight:             10,
		SourceSubmissionIntervalBlocks: 5,
		PendingDelayBlocks:             5,
		PendingDelayCapBlocks:          constitutiontypes.MinGasPricePendingDelayCap,
		RawMinGasPrice:                 constitutiontypes.MinGasPriceScaleFactor,
		PreviousMinGasPrice:            "630000000000.000000000000000000",
	}
}

func mustGenesisFieldsFromState(t *testing.T, state *constitutiontypes.GenesisState) map[string]json.RawMessage {
	t.Helper()

	fields := make(map[string]json.RawMessage)
	require.NoError(t, writeGenesisState(newGenesisTarget(fields), state))
	return fields
}

func newGenesisSource(fields map[string]json.RawMessage) appmodule.GenesisSource {
	return func(fieldName string) (io.ReadCloser, error) {
		raw, ok := fields[fieldName]
		if !ok {
			return nil, nil
		}

		return io.NopCloser(bytes.NewReader(raw)), nil
	}
}

func newGenesisTarget(fields map[string]json.RawMessage) appmodule.GenesisTarget {
	return func(fieldName string) (io.WriteCloser, error) {
		return &genesisFieldWriter{
			fieldName: fieldName,
			fields:    fields,
		}, nil
	}
}

type genesisFieldWriter struct {
	buffer    bytes.Buffer
	fieldName string
	fields    map[string]json.RawMessage
}

func (w *genesisFieldWriter) Write(p []byte) (int, error) {
	return w.buffer.Write(p)
}

func (w *genesisFieldWriter) Close() error {
	w.fields[w.fieldName] = append([]byte(nil), w.buffer.Bytes()...)
	return nil
}

func TestDefaultGenesisRequiresExplicitAddresses(t *testing.T) {
	f := setupGenesisTestFixture(t)

	fields := make(map[string]json.RawMessage)
	require.NoError(t, f.module.DefaultGenesis(newGenesisTarget(fields)))

	defaultGenesis, err := readGenesisState(newGenesisSource(fields), f.module.defaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, "", defaultGenesis.GetBaseAddress())
	require.Equal(t, "", defaultGenesis.GetModeratorAddress())
	require.Equal(t, uint32(0), defaultGenesis.GetSeparationRatio().GetBasePpm())
	require.Equal(t, uint32(0), defaultGenesis.GetSeparationRatio().GetBurnPpm())
	require.Equal(t, constitutiontypes.SeparationRatioScalePPM, defaultGenesis.GetSeparationRatio().GetValidatorsPpm())

	err = f.module.ValidateGenesis(newGenesisSource(fields))
	require.Error(t, err)
	require.ErrorContains(t, err, "base_address cannot be empty")
}

func TestValidateGenesisRequiresAddressFieldsToBePresent(t *testing.T) {
	f := setupGenesisTestFixture(t)
	state := validGenesisState(f)

	tests := []struct {
		name         string
		missingField string
	}{
		{
			name:         "fails when base_address is missing",
			missingField: "base_address",
		},
		{
			name:         "fails when moderator_address is missing",
			missingField: "moderator_address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := mustGenesisFieldsFromState(t, state)
			delete(fields, tc.missingField)

			err := f.module.ValidateGenesis(newGenesisSource(fields))
			require.Error(t, err)
			require.ErrorContains(t, err, tc.missingField+" genesis field must be explicitly set")
		})
	}
}

func TestValidateGenesisRejectsAuthorityAddressForBaseAndModerator(t *testing.T) {
	f := setupGenesisTestFixture(t)

	tests := []struct {
		name  string
		state *constitutiontypes.GenesisState
	}{
		{
			name: "fails when base_address equals authority",
			state: &constitutiontypes.GenesisState{
				Params:           validGenesisState(f).GetParams(),
				BaseAddress:      f.authorityAddress,
				ModeratorAddress: f.moderatorAddress,
				SeparationRatio:  validGenesisState(f).GetSeparationRatio(),
			},
		},
		{
			name: "fails when moderator_address equals authority",
			state: &constitutiontypes.GenesisState{
				Params:           validGenesisState(f).GetParams(),
				BaseAddress:      f.baseAddress,
				ModeratorAddress: f.authorityAddress,
				SeparationRatio:  validGenesisState(f).GetSeparationRatio(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := f.module.ValidateGenesis(newGenesisSource(mustGenesisFieldsFromState(t, tc.state)))
			require.Error(t, err)
			require.ErrorContains(t, err, "cannot equal authority")
		})
	}
}

func TestValidateGenesisRejectsInvalidSeparationRatio(t *testing.T) {
	f := setupGenesisTestFixture(t)
	state := validGenesisState(f)
	state.SeparationRatio = &constitutiontypes.SeparationRatio{
		BasePpm:       200_000,
		BurnPpm:       300_000,
		ValidatorsPpm: 400_000,
	}

	err := f.module.ValidateGenesis(newGenesisSource(mustGenesisFieldsFromState(t, state)))
	require.Error(t, err)
	require.ErrorContains(t, err, "separation_ratio total must be exactly")
}

func TestValidateGenesisRejectsInvalidPendingMinGasPrice(t *testing.T) {
	f := setupGenesisTestFixture(t)
	state := validGenesisState(f)
	state.PendingMinGasPrice = validPendingMinGasPriceSchedule()
	state.PendingMinGasPrice.RawMinGasPrice = "1"

	err := f.module.ValidateGenesis(newGenesisSource(mustGenesisFieldsFromState(t, state)))
	require.Error(t, err)
	require.ErrorContains(t, err, "raw_min_gas_price does not match")
}

func TestInitGenesisAndExportGenesisRoundTrip(t *testing.T) {
	f := setupGenesisTestFixture(t)
	state := validGenesisState(f)
	state.Params = &constitutiontypes.Params{
		MinValidatorBondAmount: testGenesisMinBond(15),
	}
	state.SeparationRatio = &constitutiontypes.SeparationRatio{
		BasePpm:       250_000,
		BurnPpm:       250_000,
		ValidatorsPpm: 500_000,
	}
	state.PendingMinGasPrice = validPendingMinGasPriceSchedule()

	require.NoError(t, f.module.InitGenesis(f.ctx, newGenesisSource(mustGenesisFieldsFromState(t, state))))

	params, err := f.keeper.GetParams(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "15", params.GetMinValidatorBondAmount().Amount.String())

	baseAddress, err := f.keeper.GetBaseAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, state.GetBaseAddress(), baseAddress)

	moderatorAddress, err := f.keeper.GetModeratorAddress(f.ctx)
	require.NoError(t, err)
	require.Equal(t, state.GetModeratorAddress(), moderatorAddress)

	separationRatio, err := f.keeper.GetSeparationRatio(f.ctx)
	require.NoError(t, err)
	require.Equal(t, state.GetSeparationRatio().GetBasePpm(), separationRatio.GetBasePpm())
	require.Equal(t, state.GetSeparationRatio().GetBurnPpm(), separationRatio.GetBurnPpm())
	require.Equal(t, state.GetSeparationRatio().GetValidatorsPpm(), separationRatio.GetValidatorsPpm())

	pendingMinGasPrice, err := f.keeper.GetMinGasPriceSchedule(f.ctx)
	require.NoError(t, err)
	require.Equal(t, state.GetPendingMinGasPrice(), pendingMinGasPrice)

	exportedFields := make(map[string]json.RawMessage)
	require.NoError(t, f.module.ExportGenesis(f.ctx, newGenesisTarget(exportedFields)))

	exportedGenesis, err := readGenesisState(newGenesisSource(exportedFields), f.module.defaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, state.GetBaseAddress(), exportedGenesis.GetBaseAddress())
	require.Equal(t, state.GetModeratorAddress(), exportedGenesis.GetModeratorAddress())
	require.Equal(t, state.GetParams().GetMinValidatorBondAmount().Amount, exportedGenesis.GetParams().GetMinValidatorBondAmount().Amount)
	require.Equal(t, state.GetSeparationRatio().GetBasePpm(), exportedGenesis.GetSeparationRatio().GetBasePpm())
	require.Equal(t, state.GetSeparationRatio().GetBurnPpm(), exportedGenesis.GetSeparationRatio().GetBurnPpm())
	require.Equal(t, state.GetSeparationRatio().GetValidatorsPpm(), exportedGenesis.GetSeparationRatio().GetValidatorsPpm())
	require.Equal(t, state.GetPendingMinGasPrice(), exportedGenesis.GetPendingMinGasPrice())
}

func TestExportGenesisFailsWhenBaseOrModeratorAddressMissing(t *testing.T) {
	tests := []struct {
		name                string
		setBaseAddress      bool
		setModeratorAddress bool
	}{
		{
			name:                "fails when base address is missing",
			setBaseAddress:      false,
			setModeratorAddress: true,
		},
		{
			name:                "fails when moderator address is missing",
			setBaseAddress:      true,
			setModeratorAddress: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupGenesisTestFixture(t)
			require.NoError(t, f.keeper.SetParams(f.ctx, validGenesisState(f).GetParams()))
			require.NoError(t, f.keeper.SetSeparationRatio(f.ctx, validGenesisState(f).GetSeparationRatio()))

			if tc.setBaseAddress {
				require.NoError(t, f.keeper.SetBaseAddress(f.ctx, f.baseAddress))
			}
			if tc.setModeratorAddress {
				require.NoError(t, f.keeper.SetModeratorAddress(f.ctx, f.moderatorAddress))
			}

			err := f.module.ExportGenesis(f.ctx, newGenesisTarget(make(map[string]json.RawMessage)))
			require.Error(t, err)
			require.ErrorIs(t, err, collections.ErrNotFound)
		})
	}
}

func TestInitGenesisFailsWhenRequiredAddressesMissing(t *testing.T) {
	f := setupGenesisTestFixture(t)
	state := validGenesisState(f)

	tests := []struct {
		name         string
		missingField string
	}{
		{
			name:         "fails when base_address is missing",
			missingField: "base_address",
		},
		{
			name:         "fails when moderator_address is missing",
			missingField: "moderator_address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := mustGenesisFieldsFromState(t, state)
			delete(fields, tc.missingField)

			err := f.module.InitGenesis(f.ctx, newGenesisSource(fields))
			require.Error(t, err)
			require.ErrorContains(t, err, tc.missingField+" genesis field must be explicitly set")
		})
	}
}
