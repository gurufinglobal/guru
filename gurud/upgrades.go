package gurud

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

func (app EVMD) RegisterUpgradeHandlers() {
	// v201-to-v202 upgrade handler
	app.UpgradeKeeper.SetUpgradeHandler(
		"v201-to-v202",
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// Upgrade logic here
			// For a simple upgrade without state migrations, just return the current version map
			return app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
		},
	)
}
