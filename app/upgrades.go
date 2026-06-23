package app

import (
	"context"
	"os"

	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

const upgradeNameV1 = "v1"
const envEnableUpgradeHandlerV1 = "GURU_ENABLE_UPGRADE_HANDLER_V1"

// RegisterUpgradeHandlers is used for registering on-chain upgrades.
func (app *App) RegisterUpgradeHandlers() {
	if os.Getenv(envEnableUpgradeHandlerV1) != "1" {
		return
	}

	app.UpgradeKeeper.SetUpgradeHandler(
		upgradeNameV1,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			return app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
		},
	)
}
