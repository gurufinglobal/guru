package gurud

import (
	"context"

	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

func (app EVMD) RegisterUpgradeHandlers() {
	// v201-to-v202 upgrade handler
	app.UpgradeKeeper.SetUpgradeHandler(
		"v201-to-v202",
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// Log the upgrade execution
			app.Logger().Info("Executing v201-to-v202 upgrade", "plan", plan.Name, "height", plan.Height)

			sdkCtx := sdk.UnwrapSDKContext(ctx)

			// Migrate feemarket params
			// min_gas_price_rate -> gas_price_adjustment_factor (field renamed)
			// Add max_change_rate field (new field)
			sdkCtx.Logger().Info("Starting feemarket params migration")

			params := app.FeeMarketKeeper.GetParams(sdkCtx)

			// Store the old min_gas_price_rate value before it's overwritten
			// Since the field number (9) is the same, the value should already be loaded
			// into GasPriceAdjustmentFactor automatically by protobuf
			oldMinGasPriceRate := params.GasPriceAdjustmentFactor

			sdkCtx.Logger().Info("Current params",
				"gas_price_adjustment_factor", params.GasPriceAdjustmentFactor.String(),
				"max_change_rate", params.MaxChangeRate.String(),
			)

			// If GasPriceAdjustmentFactor is loaded from old min_gas_price_rate,
			// it should already have the value. If it's zero, set default.
			if params.GasPriceAdjustmentFactor.IsZero() {
				params.GasPriceAdjustmentFactor = math.LegacyOneDec()
				sdkCtx.Logger().Info("Set GasPriceAdjustmentFactor to default (was zero)", "value", params.GasPriceAdjustmentFactor.String())
			} else {
				// Value was successfully migrated from min_gas_price_rate
				sdkCtx.Logger().Info("Migrated min_gas_price_rate to gas_price_adjustment_factor",
					"old_value", oldMinGasPriceRate.String(),
					"new_value", params.GasPriceAdjustmentFactor.String(),
				)
			}

			// If MaxChangeRate is not set (zero), use default value (0.1 = 10%)
			if params.MaxChangeRate.IsZero() {
				params.MaxChangeRate = math.LegacyNewDecWithPrec(1, 1) // 0.1
				sdkCtx.Logger().Info("Set MaxChangeRate to default", "value", params.MaxChangeRate.String())
			}

			// Save updated params
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, params); err != nil {
				sdkCtx.Logger().Error("Failed to set feemarket params", "error", err)
				return nil, err
			}

			sdkCtx.Logger().Info("Completed feemarket params migration")

			// Run module migrations
			return app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
		},
	)
}
