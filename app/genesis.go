package app

import "encoding/json"

// GenesisState is intentionally empty in Stage A because no modules are wired.
type GenesisState map[string]json.RawMessage

// DefaultGenesis returns a fresh, non-nil empty genesis map.
func DefaultGenesis() GenesisState {
	return GenesisState{}
}

// DefaultGenesis returns a fresh Stage A genesis state for this application.
func (app *App) DefaultGenesis() GenesisState {
	return DefaultGenesis()
}
