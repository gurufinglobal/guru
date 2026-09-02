package app

import (
	"errors"
	"fmt"

	oracleabci "github.com/gurufinglobal/guru/v2/x/oracle/abci"
	"github.com/spf13/cast"
)

func (app *App) configureOracleConsensus(appOptions AppOptions) error {
	proposalHandler := newStandardMsgSendProposalHandler(app)
	aggregator := oracleabci.NewAggregator(app.OracleKeeper, app.StakingKeeper)
	oracleProposalHandler := oracleabci.NewProposalHandler(
		aggregator,
		proposalHandler.PrepareProposal,
		proposalHandler.ProcessProposal,
	)
	app.SetPrepareProposal(oracleProposalHandler.PrepareProposal)
	app.SetProcessProposal(oracleProposalHandler.ProcessProposal)
	app.OracleProposalHandler = &oracleProposalHandler

	oracleEnabled := true
	var socket string
	var timeoutValue any
	if appOptions != nil {
		if value := appOptions.Get("oracle.enabled"); value != nil {
			oracleEnabled = cast.ToBool(value)
		}
		socket = cast.ToString(appOptions.Get("oracle.sidecar_socket"))
		timeoutValue = appOptions.Get("oracle.sidecar_timeout")
	}
	oracleVoteHandler, err := oracleabci.NewVoteExtensionHandler(
		app.OracleKeeper,
		oracleEnabled,
		socket,
		cast.ToDuration(timeoutValue),
	)
	if err != nil {
		return fmt.Errorf("configure oracle vote extension handler: %w", err)
	}
	app.oracleVoteHandler = oracleVoteHandler
	app.SetExtendVoteHandler(oracleVoteHandler.ExtendVote)
	app.SetVerifyVoteExtensionHandler(oracleVoteHandler.VerifyVoteExtension)

	return nil
}

// Close releases the optional sidecar connection before closing BaseApp.
func (app *App) Close() error {
	var err error
	if app.oracleVoteHandler != nil {
		err = errors.Join(err, app.oracleVoteHandler.Close())
	}
	return errors.Join(err, app.BaseApp.Close())
}
