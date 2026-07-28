package types

// NewGenesisState constructs a feepolicy genesis state.
func NewGenesisState(moderatorAddress string, discounts []AccountDiscount) GenesisState {
	return GenesisState{ModeratorAddress: moderatorAddress, Discounts: discounts}
}

// DefaultGenesisState is structurally valid. An empty legacy moderator field
// means that Constitution supplies the single authoritative moderator.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{Discounts: []AccountDiscount{}}
}

// Validate performs codec-independent structural validation. Address
// validation is performed by AppModule with its injected EVM address codec.
func (gs GenesisState) Validate() error {
	for _, discount := range gs.Discounts {
		if err := ValidateAccountDiscount(discount); err != nil {
			return err
		}
	}
	return nil
}
