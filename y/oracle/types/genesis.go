package types

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/types"
)

// NewGenesisState creates a new GenesisState object.
func NewGenesisState(params Params, moderator string, requests []OracleRequest, categories []Category) *GenesisState {
	return &GenesisState{
		Params:           params,
		ModeratorAddress: moderator,
		Requests:         requests,
		Categories:       categories,
	}
}

// DefaultGenesisState returns a default GenesisState object.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:           DefaultParams(),
		ModeratorAddress: "",
		Requests:         []OracleRequest{},
		Categories:       []Category{},
	}
}

// Validate performs basic validation of the genesis state.
func (g GenesisState) Validate() error {
	if err := g.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	if g.ModeratorAddress != "" {
		if _, err := types.AccAddressFromBech32(g.ModeratorAddress); err != nil {
			return fmt.Errorf("invalid moderator address: %w", err)
		}
	}

	for _, cat := range g.Categories {
		if cat == Category_CATEGORY_UNSPECIFIED {
			return fmt.Errorf("category cannot be unspecified")
		}
		if !IsKnownCategory(cat) {
			return fmt.Errorf("unknown category enum value: %d", int32(cat))
		}
	}

	seenIDs := make(map[uint64]struct{})
	for _, req := range g.Requests {
		if err := req.ValidateBasic(); err != nil {
			return fmt.Errorf("invalid request %d: %w", req.Id, err)
		}
		if _, ok := seenIDs[req.Id]; ok {
			return fmt.Errorf("duplicate request id %d", req.Id)
		}
		if !IsKnownCategory(req.Category) {
			return fmt.Errorf("request %d uses unknown category enum value: %d", req.Id, int32(req.Category))
		}
		seenIDs[req.Id] = struct{}{}
	}

	return nil
}
