package keeper

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

var _ oracletypes.OracleHooks = OracleHooks{}

type OracleHooks struct {
	k Keeper
}

func (k Keeper) OracleHooks() OracleHooks {
	return OracleHooks{k}
}

func (h OracleHooks) AfterOracleAggregation(ctx sdk.Context, request oracletypes.OracleRequest, result oracletypes.OracleResult) {
	if request.Category != oracletypes.Category_CATEGORY_OPERATION {
		return
	}

	// TODO: add filter for min gas price

	if result.AggregatedData == "" {
		return
	}

	params := h.k.GetParams(ctx)

	gasPriceAdjustmentFactor := params.GasPriceAdjustmentFactor
	if gasPriceAdjustmentFactor.IsZero() {
		return
	}

	oracleResultDec, err := math.LegacyNewDecFromStr(result.AggregatedData)
	if err != nil {
		h.k.Logger(ctx).Error("failed to parse oracle result", "err", err)
		return
	}

	if oracleResultDec.IsZero() {
		return
	}

	rawNewPriceDec := gasPriceAdjustmentFactor.Quo(oracleResultDec)

	newPriceInt := rawNewPriceDec.TruncateInt()

	if newPriceInt.IsZero() && !rawNewPriceDec.IsZero() {
		newPriceInt = math.OneInt()
	}

	currentMinGasPrice := params.MinGasPrice
	maxChangeRate := params.MaxChangeRate

	if !maxChangeRate.IsZero() && !currentMinGasPrice.IsZero() {
		maxChange := currentMinGasPrice.Mul(maxChangeRate)
		upperBoundDec := currentMinGasPrice.Add(maxChange)
		lowerBoundDec := currentMinGasPrice.Sub(maxChange)

		if lowerBoundDec.IsNegative() {
			lowerBoundDec = math.LegacyZeroDec()
		}

		newPriceDecCheck := math.LegacyNewDecFromInt(newPriceInt)

		if newPriceDecCheck.GT(upperBoundDec) {
			newPriceInt = upperBoundDec.TruncateInt()
		} else if newPriceDecCheck.LT(lowerBoundDec) {
			newPriceInt = lowerBoundDec.TruncateInt()
		}
	}

	finalNewPriceDec := math.LegacyNewDecFromInt(newPriceInt)
	if finalNewPriceDec.Equal(params.MinGasPrice) {
		return
	}

	params.MinGasPrice = finalNewPriceDec
	h.k.SetParams(ctx, params)

	// ctx.EventManager().EmitEvent(
	//     sdk.NewEvent(
	//     ),
	// )
}
