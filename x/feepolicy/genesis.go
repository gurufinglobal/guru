package feepolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"cosmossdk.io/core/appmodule"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/keeper"
	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

func (am AppModule) DefaultGenesis(target appmodule.GenesisTarget) error {
	return writeGenesisState(target, &types.GenesisState{
		Discounts: []types.AccountDiscount{},
	})
}

func (am AppModule) ValidateGenesis(source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	_, err = normalizeGenesis(am.keeper, *genesis)
	return err
}

func (am AppModule) InitGenesis(ctx context.Context, source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	return InitGenesis(ctx, am.keeper, *genesis)
}

func (am AppModule) ExportGenesis(ctx context.Context, target appmodule.GenesisTarget) error {
	genesis, err := ExportGenesis(ctx, am.keeper)
	if err != nil {
		return err
	}
	return writeGenesisState(target, &genesis)
}

func (am AppModule) defaultGenesisState() *types.GenesisState {
	return &types.GenesisState{
		Discounts: []types.AccountDiscount{},
	}
}

// InitGenesis imports only policy state. The legacy moderator field may be
// omitted to inherit Constitution, or supplied as a decoded-byte equality
// assertion against Constitution's already-initialized moderator.
func InitGenesis(ctx context.Context, k keeper.Keeper, data types.GenesisState) error {
	normalized, err := normalizeGenesis(k, data)
	if err != nil {
		return err
	}
	constitutionModerator, err := currentConstitutionModerator(ctx, k)
	if err != nil {
		return err
	}
	if normalized.ModeratorAddress != "" && normalized.ModeratorAddress != constitutionModerator {
		return fmt.Errorf(
			"feepolicy moderator address %q does not match Constitution moderator %q",
			normalized.ModeratorAddress,
			constitutionModerator,
		)
	}
	for _, discount := range normalized.Discounts {
		if err := k.SetAccountDiscounts(ctx, discount); err != nil {
			return err
		}
	}
	return nil
}

func ExportGenesis(ctx context.Context, k keeper.Keeper) (types.GenesisState, error) {
	moderator, err := currentConstitutionModerator(ctx, k)
	if err != nil {
		return types.GenesisState{}, err
	}
	discounts, err := k.GetAllDiscounts(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	return types.NewGenesisState(moderator, discounts), nil
}

func normalizeGenesis(k keeper.Keeper, data types.GenesisState) (types.GenesisState, error) {
	if err := data.Validate(); err != nil {
		return types.GenesisState{}, err
	}
	moderator := data.ModeratorAddress
	canonicalModerator := ""
	var err error
	if moderator != "" {
		canonicalModerator, err = k.CanonicalModeratorAddress(moderator)
		if err != nil {
			return types.GenesisState{}, fmt.Errorf("invalid feepolicy moderator address: %w", err)
		}
	}

	normalized := types.GenesisState{
		ModeratorAddress: canonicalModerator,
		Discounts:        make([]types.AccountDiscount, len(data.Discounts)),
	}
	accounts := make(map[string]struct{}, len(data.Discounts))
	for i, discount := range data.Discounts {
		normalized.Discounts[i], err = k.NormalizeAccountDiscount(discount)
		if err != nil {
			return types.GenesisState{}, fmt.Errorf("invalid feepolicy discount %d: %w", i, err)
		}
		if _, exists := accounts[normalized.Discounts[i].Address]; exists {
			return types.GenesisState{}, fmt.Errorf(
				"duplicate feepolicy discount address %q",
				normalized.Discounts[i].Address,
			)
		}
		accounts[normalized.Discounts[i].Address] = struct{}{}
	}
	return normalized, nil
}

func currentConstitutionModerator(ctx context.Context, k keeper.Keeper) (string, error) {
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return "", fmt.Errorf("read Constitution moderator address: %w", err)
	}
	canonical, err := k.CanonicalModeratorAddress(moderator)
	if err != nil {
		return "", fmt.Errorf("invalid Constitution moderator address: %w", err)
	}
	return canonical, nil
}

func readGenesisState(source appmodule.GenesisSource, defaults *types.GenesisState) (*types.GenesisState, error) {
	if source == nil {
		return nil, fmt.Errorf("feepolicy genesis source cannot be nil")
	}
	genesis := &types.GenesisState{
		ModeratorAddress: defaults.GetModeratorAddress(),
		Discounts:        append([]types.AccountDiscount(nil), defaults.GetDiscounts()...),
	}

	moderator := genesis.ModeratorAddress
	found, err := readGenesisField(source, "moderator_address", &moderator)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.ModeratorAddress = moderator
	}

	discounts := []types.AccountDiscount{}
	found, err = readGenesisField(source, "discounts", &discounts)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.Discounts = discounts
	}
	return genesis, nil
}

func writeGenesisState(target appmodule.GenesisTarget, genesis *types.GenesisState) error {
	if target == nil {
		return fmt.Errorf("feepolicy genesis target cannot be nil")
	}
	if genesis == nil {
		return fmt.Errorf("feepolicy genesis state cannot be nil")
	}
	if err := writeGenesisField(target, "moderator_address", genesis.GetModeratorAddress()); err != nil {
		return err
	}
	return writeGenesisField(target, "discounts", genesis.GetDiscounts())
}

func readGenesisField(source appmodule.GenesisSource, field string, value any) (bool, error) {
	reader, err := source(field)
	if err != nil {
		return false, fmt.Errorf("open feepolicy genesis field %q: %w", field, err)
	}
	if reader == nil {
		return false, nil
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		_ = reader.Close()
		return false, fmt.Errorf("decode feepolicy genesis field %q: %w", field, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		_ = reader.Close()
		if err == nil {
			return false, fmt.Errorf("decode feepolicy genesis field %q: unexpected trailing JSON value", field)
		}
		return false, fmt.Errorf("decode feepolicy genesis field %q: %w", field, err)
	}
	if err := reader.Close(); err != nil {
		return false, fmt.Errorf("close feepolicy genesis field %q: %w", field, err)
	}
	return true, nil
}

func writeGenesisField(target appmodule.GenesisTarget, field string, value any) error {
	writer, err := target(field)
	if err != nil {
		return fmt.Errorf("open feepolicy genesis target field %q: %w", field, err)
	}
	if writer == nil {
		return fmt.Errorf("feepolicy genesis target field %q returned a nil writer", field)
	}
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return fmt.Errorf("encode feepolicy genesis field %q: %w", field, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close feepolicy genesis field %q: %w", field, err)
	}
	return nil
}
