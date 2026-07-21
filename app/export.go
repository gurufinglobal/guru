package app

import (
	"encoding/json"
	"errors"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

// ExportAppStateAndValidators exports the state of the application for a genesis
// file.
func (app *App) ExportAppStateAndValidators(forZeroHeight bool, jailAllowedAddrs []string, modulesToExport []string) (servertypes.ExportedApp, error) {
	// The SDK exporter still carries zero-height options in its interface. Guru
	// only supports height-preserving exports because module schedules use
	// absolute block heights.
	if forZeroHeight {
		return servertypes.ExportedApp{}, errors.New("zero-height export is not supported; omit --for-zero-height to preserve block heights")
	}
	if len(jailAllowedAddrs) > 0 {
		return servertypes.ExportedApp{}, errors.New("--jail-allowed-addrs is not supported because zero-height export is disabled")
	}

	// as if they could withdraw from the start of the next block
	ctx := app.NewContextLegacy(true, tmproto.Header{Height: app.LastBlockHeight()})

	// We export at last height + 1, because that's the height at which
	// CometBFT will start InitChain.
	height := app.LastBlockHeight() + 1

	genState, err := app.ModuleManager.ExportGenesisForModules(ctx, app.appCodec, modulesToExport)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	appState, err := json.MarshalIndent(genState, "", "  ")
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	consensusParams := app.GetConsensusParams(ctx)
	if storedConsensusParams, err := app.ConsensusParamsKeeper.ParamsStore.Get(ctx); err == nil {
		consensusParams = storedConsensusParams
	}
	consensusParams = ensureExportConsensusParams(consensusParams, height)

	return servertypes.ExportedApp{
		AppState:        appState,
		Validators:      validators,
		Height:          height,
		ConsensusParams: consensusParams,
	}, err
}

func ensureExportConsensusParams(params tmproto.ConsensusParams, initialHeight int64) tmproto.ConsensusParams {
	if params.Abci == nil {
		params.Abci = &tmproto.ABCIParams{}
	}
	minEnableHeight := initialHeight
	if minEnableHeight < 1 {
		minEnableHeight = 1
	}
	if params.Abci.VoteExtensionsEnableHeight == 0 || params.Abci.VoteExtensionsEnableHeight < minEnableHeight {
		params.Abci.VoteExtensionsEnableHeight = minEnableHeight
	}
	return params
}
