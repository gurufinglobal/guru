package app

import (
	"encoding/json"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

// ExportAppStateAndValidators exports the currently loaded state at the next
// genesis height. Zero-height state rewriting is deliberately not implicit;
// it requires staking, distribution, and slashing transformations that are
// outside this bootstrap runtime.
func (app *App) ExportAppStateAndValidators(
	forZeroHeight bool,
	jailAllowedAddrs []string,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	if forZeroHeight {
		return servertypes.ExportedApp{}, fmt.Errorf("zero-height export is not supported")
	}
	if len(jailAllowedAddrs) != 0 {
		return servertypes.ExportedApp{}, fmt.Errorf("jail allowlist requires zero-height export")
	}

	ctx := app.NewContextLegacy(true, cmtproto.Header{
		Height:  app.LastBlockHeight(),
		ChainID: app.ChainID(),
	})
	genesis, err := app.ModuleManager.ExportGenesisForModules(ctx, app.AppCodec(), modulesToExport)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	// A complete export must be accepted by the same general validator used by
	// InitChain. Partial module exports are intentionally not bootable documents.
	if len(modulesToExport) == 0 {
		if err := app.ValidateGenesis(GenesisState(genesis)); err != nil {
			return servertypes.ExportedApp{}, fmt.Errorf("validate exported application state: %w", err)
		}
	}
	appState, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	return servertypes.ExportedApp{
		AppState:        appState,
		Validators:      validators,
		Height:          app.LastBlockHeight() + 1,
		ConsensusParams: app.GetConsensusParams(ctx),
	}, nil
}
