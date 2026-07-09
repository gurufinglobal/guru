package app

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	"github.com/spf13/cast"
)

func (app *App) configureOracleVoteExtensions(appOpts servertypes.AppOptions) {
	oracleEnabled := true
	if value := appOpts.Get("oracle.enabled"); value != nil {
		oracleEnabled = cast.ToBool(value)
	}

	oracleVoteHandler := oracleabci.NewVoteExtensionHandler(
		app.OracleKeeper,
		oracleEnabled,
		cast.ToString(appOpts.Get("oracle.sidecar_socket")),
		cast.ToDuration(appOpts.Get("oracle.sidecar_timeout")),
	)
	app.SetExtendVoteHandler(oracleVoteHandler.ExtendVote)
	app.SetVerifyVoteExtensionHandler(oracleVoteHandler.VerifyVoteExtension)
}
