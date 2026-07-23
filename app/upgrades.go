package app

import (
	"context"
	"fmt"
	"os"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

const upgradeNameV1 = "v1"
const envEnableUpgradeHandlerV1 = "GURU_ENABLE_UPGRADE_HANDLER_V1"

// RegisterUpgradeHandlers is used for registering on-chain upgrades.
func (app *App) RegisterUpgradeHandlers() {
	// Only the same-binary upgrade rehearsal disables this handler explicitly.
	// Production binaries must retain completed handlers across every restart.
	if os.Getenv(envEnableUpgradeHandlerV1) == "0" {
		return
	}

	app.UpgradeKeeper.SetUpgradeHandler(
		upgradeNameV1,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			return app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Errorf("failed to read upgrade info from disk: %w", err))
	}
	storeUpgrades := storeUpgradesForPlan(upgradeInfo.Name)
	if storeUpgrades == nil || app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return
	}
	app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, storeUpgrades))
}

func storeUpgradesForPlan(name string) *storetypes.StoreUpgrades {
	switch name {
	case upgradeNameV1:
		return &storetypes.StoreUpgrades{Added: []string{bextypes.StoreKey, transwaptypes.StoreKey}}
	default:
		return nil
	}
}
