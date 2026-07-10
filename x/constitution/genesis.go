package constitution

import (
	"context"
	"encoding/json"
	"errors"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/collections"
	"cosmossdk.io/core/appmodule"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutionkeeper "github.com/gurufinglobal/guru/v3/x/constitution/keeper"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// DefaultGenesis writes default genesis in core GenesisTarget form.
func (am AppModule) DefaultGenesis(target appmodule.GenesisTarget) error {
	return writeGenesisState(target, am.defaultGenesisState())
}

// ValidateGenesis validates provided genesis in core GenesisSource form.
func (am AppModule) ValidateGenesis(source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	return am.validateGenesisState(genesis)
}

// InitGenesis initializes store state from genesis.
func (am AppModule) InitGenesis(ctx context.Context, source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	if err := am.validateGenesisState(genesis); err != nil {
		return err
	}
	if err := am.keeper.SetParams(ctx, genesis.Params); err != nil {
		return err
	}
	if err := am.keeper.SetBaseAddress(ctx, genesis.BaseAddress); err != nil {
		return err
	}
	if err := am.keeper.SetModeratorAddress(ctx, genesis.ModeratorAddress); err != nil {
		return err
	}
	if genesis.PendingMinGasPrice != nil {
		if err := am.keeper.SetMinGasPriceSchedule(ctx, genesis.PendingMinGasPrice); err != nil {
			return err
		}
	}

	return am.keeper.SetSeparationRatio(ctx, genesis.SeparationRatio)
}

// ExportGenesis writes current module state to genesis target.
func (am AppModule) ExportGenesis(ctx context.Context, target appmodule.GenesisTarget) error {
	defaults := am.defaultGenesisState()

	params, err := am.keeper.GetParams(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		params = defaults.Params
	}

	baseAddress, err := am.keeper.GetBaseAddress(ctx)
	if err != nil {
		return err
	}

	moderatorAddress, err := am.keeper.GetModeratorAddress(ctx)
	if err != nil {
		return err
	}

	separationRatio, err := am.keeper.GetSeparationRatio(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		separationRatio = defaults.SeparationRatio
	}

	pendingMinGasPrice, err := am.keeper.GetMinGasPriceSchedule(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
	}

	return writeGenesisState(target, &constitutionv1.GenesisState{
		Params:             params,
		BaseAddress:        baseAddress,
		ModeratorAddress:   moderatorAddress,
		SeparationRatio:    separationRatio,
		PendingMinGasPrice: pendingMinGasPrice,
	})
}

func (am AppModule) defaultGenesisState() *constitutionv1.GenesisState {
	return &constitutionv1.GenesisState{
		Params: &constitutionv1.Params{
			MinValidatorBondAmount: &basev1beta1.Coin{
				Denom:  appparams.BaseDenom,
				Amount: "10",
			},
		},
		// base_address and moderator_address must be explicitly configured in genesis.
		BaseAddress:      "",
		ModeratorAddress: "",
		SeparationRatio: &constitutionv1.SeparationRatio{
			BasePpm:       0,
			BurnPpm:       0,
			ValidatorsPpm: constitutiontypes.SeparationRatioScalePPM,
		},
	}
}

func (am AppModule) validateGenesisState(data *constitutionv1.GenesisState) error {
	if data == nil {
		return constitutiontypes.ErrInvalidParams.Wrap("genesis state cannot be nil")
	}

	if err := constitutionkeeper.ValidateParams(data.Params); err != nil {
		return err
	}
	if err := am.keeper.ValidateBaseAddress(data.GetBaseAddress()); err != nil {
		return err
	}
	if err := am.keeper.ValidateModeratorAddress(data.GetModeratorAddress()); err != nil {
		return err
	}
	if data.PendingMinGasPrice != nil {
		if err := am.keeper.ValidateMinGasPriceSchedule(data.PendingMinGasPrice); err != nil {
			return err
		}
	}

	return constitutionkeeper.ValidateSeparationRatio(data.GetSeparationRatio())
}

func readGenesisState(source appmodule.GenesisSource, defaults *constitutionv1.GenesisState) (*constitutionv1.GenesisState, error) {
	genesis := &constitutionv1.GenesisState{
		Params:             defaults.Params,
		BaseAddress:        defaults.BaseAddress,
		ModeratorAddress:   defaults.ModeratorAddress,
		SeparationRatio:    defaults.SeparationRatio,
		PendingMinGasPrice: defaults.PendingMinGasPrice,
	}

	params := &constitutionv1.Params{}
	found, err := readGenesisField(source, "params", params)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.Params = params
	}

	var baseAddress string
	if err := readRequiredGenesisField(source, "base_address", &baseAddress); err != nil {
		return nil, err
	}
	genesis.BaseAddress = baseAddress

	var moderatorAddress string
	if err := readRequiredGenesisField(source, "moderator_address", &moderatorAddress); err != nil {
		return nil, err
	}
	genesis.ModeratorAddress = moderatorAddress

	separationRatio := &constitutionv1.SeparationRatio{}
	found, err = readGenesisField(source, "separation_ratio", separationRatio)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.SeparationRatio = separationRatio
	}

	pendingMinGasPrice := &constitutionv1.MinGasPriceSchedule{}
	found, err = readGenesisField(source, "pending_min_gas_price", pendingMinGasPrice)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.PendingMinGasPrice = pendingMinGasPrice
	}

	return genesis, nil
}

func writeGenesisState(target appmodule.GenesisTarget, genesis *constitutionv1.GenesisState) error {
	if genesis == nil {
		return constitutiontypes.ErrInvalidParams.Wrap("genesis state cannot be nil")
	}

	if err := writeGenesisField(target, "params", genesis.Params); err != nil {
		return err
	}
	if err := writeGenesisField(target, "base_address", genesis.BaseAddress); err != nil {
		return err
	}
	if err := writeGenesisField(target, "moderator_address", genesis.ModeratorAddress); err != nil {
		return err
	}

	if err := writeGenesisField(target, "separation_ratio", genesis.SeparationRatio); err != nil {
		return err
	}
	if genesis.PendingMinGasPrice != nil {
		return writeGenesisField(target, "pending_min_gas_price", genesis.PendingMinGasPrice)
	}

	return nil
}

func readGenesisField(source appmodule.GenesisSource, fieldName string, value any) (bool, error) {
	reader, err := source(fieldName)
	if err != nil {
		return false, constitutiontypes.ErrReadGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	if reader == nil {
		return false, nil
	}
	defer func() { _ = reader.Close() }()

	var raw json.RawMessage
	if err := json.NewDecoder(reader).Decode(&raw); err != nil {
		return false, constitutiontypes.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}
	if msg, ok := value.(proto.Message); ok {
		if err := protojson.Unmarshal(raw, msg); err != nil {
			return false, constitutiontypes.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
		}
		return true, nil
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return false, constitutiontypes.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	return true, nil
}

func readRequiredGenesisField(source appmodule.GenesisSource, fieldName string, value any) error {
	found, err := readGenesisField(source, fieldName, value)
	if err != nil {
		return err
	}
	if !found {
		return constitutiontypes.ErrRequiredGenesisField.Wrapf("%s genesis field must be explicitly set", fieldName)
	}

	return nil
}

func writeGenesisField(target appmodule.GenesisTarget, fieldName string, value any) error {
	writer, err := target(fieldName)
	if err != nil {
		return constitutiontypes.ErrOpenGenesisTargetField.Wrapf("%s: %v", fieldName, err)
	}
	if writer == nil {
		return constitutiontypes.ErrNilGenesisTargetWriter.Wrapf("%s genesis target field writer is nil", fieldName)
	}

	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return constitutiontypes.ErrEncodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	if err := writer.Close(); err != nil {
		return constitutiontypes.ErrCloseGenesisFieldWriter.Wrapf("%s: %v", fieldName, err)
	}

	return nil
}
