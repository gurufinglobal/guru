package types

// NewGenesisState creates a new GenesisState object
func NewGenesisState(moderatorAddress string, ratemeter Ratemeter, exchanges []Exchange) GenesisState {
	return GenesisState{
		ModeratorAddress: moderatorAddress,
		Ratemeter:        ratemeter,
		Exchanges:        exchanges,
	}
}

// DefaultGenesisState creates a default GenesisState object
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		ModeratorAddress: "",
		Ratemeter:        DefaultRatemeter(),
		Exchanges:        []Exchange{},
	}
}

// Validate validates the genesis state to ensure the
// expected invariants holds.
func (gs GenesisState) Validate() error {
	// if err := validateAddress(gs.ModeratorAddress); err != nil {
	// 	return err
	// }

	for _, exchange := range gs.Exchanges {
		if err := exchange.Validate(); err != nil {
			return err
		}
	}

	return nil
}
