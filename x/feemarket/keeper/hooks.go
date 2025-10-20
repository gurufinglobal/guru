package keeper

import (
	"cosmossdk.io/math"
	"github.com/gurufinglobal/guru/v2/x/feemarket/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) BeforeOracleStart(_ sdk.Context, _ oracletypes.DataSet) {
}

// AfterOracleEnd updates the min gas price at the end of each oracle end
func (k Keeper) AfterOracleEnd(ctx sdk.Context, dataSet oracletypes.DataSet) {
	logger := ctx.Logger()
	logger.Info("AfterOracleEnd hook triggered", "dataSet", dataSet)

	params := k.GetParams(ctx)
	gasPriceAdjustmentFactor := params.GasPriceAdjustmentFactor

	if gasPriceAdjustmentFactor.IsZero() {
		return
	}

	// newMinGasPrice = gasPriceAdjustmentFactor / dataSet.RawData
	rawDataDec, err := math.LegacyNewDecFromStr(dataSet.RawData)
	if err != nil {
		logger.Error("Failed to parse oracle raw data as decimal", "rawData", dataSet.RawData, "error", err)
		return
	}

	if rawDataDec.IsZero() {
		logger.Error("Oracle raw data is zero, cannot divide", "rawData", dataSet.RawData)
		return
	}

	newMinGasPrice := gasPriceAdjustmentFactor.Quo(rawDataDec).TruncateInt()

	// Check if the new min gas price change exceeds the max change rate
	currentMinGasPrice := params.MinGasPrice
	maxChangeRate := params.MaxChangeRate

	if !maxChangeRate.IsZero() && !currentMinGasPrice.IsZero() {
		// Calculate the maximum allowed change
		maxChange := currentMinGasPrice.Mul(maxChangeRate)
		upperBound := currentMinGasPrice.Add(maxChange)
		lowerBound := currentMinGasPrice.Sub(maxChange)

		newMinGasPriceDec := math.LegacyNewDecFromInt(newMinGasPrice)

		// Apply bounds if the new price exceeds the allowed change rate
		if newMinGasPriceDec.GT(upperBound) {
			newMinGasPrice = upperBound.TruncateInt()
			logger.Info("New min gas price capped at upper bound",
				"original", newMinGasPriceDec.String(),
				"capped", upperBound.String())
		} else if newMinGasPriceDec.LT(lowerBound) {
			newMinGasPrice = lowerBound.TruncateInt()
			logger.Info("New min gas price raised to lower bound",
				"original", newMinGasPriceDec.String(),
				"raised", lowerBound.String())
		}
	}

	params.MinGasPrice = math.LegacyNewDecFromInt(newMinGasPrice)

	k.SetParams(ctx, params)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeChangeMinGasPrice,
			sdk.NewAttribute(types.AttributeKeyMinGasPrice, newMinGasPrice.String()),
		),
	)
}

// Hooks wrapper struct for feemarket keeper
type Hooks struct {
	k Keeper
}

var _ oracletypes.OracleHooks = Hooks{}

// Return the wrapper struct
func (k Keeper) Hooks() Hooks {
	return Hooks{k}
}

// oracle hooks
func (h Hooks) BeforeOracleStart(ctx sdk.Context, dataSet oracletypes.DataSet) {
	h.k.BeforeOracleStart(ctx, dataSet)
}

func (h Hooks) AfterOracleEnd(ctx sdk.Context, dataSet oracletypes.DataSet) {
	h.k.AfterOracleEnd(ctx, dataSet)
}
