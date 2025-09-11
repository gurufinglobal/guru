package keeper

import (
	"cosmossdk.io/math"
	"github.com/GPTx-global/guru-v2/x/bex/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) BeforeOracleStart(_ sdk.Context, _ oracletypes.DataSet) {
}

// AfterOracleEnd updates the min gas price at the end of each oracle end
func (k Keeper) AfterOracleEnd(ctx sdk.Context, dataSet oracletypes.DataSet) {
	logger := ctx.Logger()
	logger.Info("AfterOracleEnd hook triggered", "dataSet", dataSet)

	exchanges, err := k.GetExchangesByOracleRequestId(ctx, dataSet.RequestId)
	if err != nil {
		logger.Error("Failed to get exchanges by oracle request id", "error", err)
		return
	}

	rawDataDec, err := math.LegacyNewDecFromStr(dataSet.RawData)
	if err != nil {
		logger.Error("Failed to parse oracle raw data as decimal", "rawData", dataSet.RawData, "error", err)
		return
	}

	for _, exchange := range exchanges {
		exchange.Rate = rawDataDec
		k.SetExchange(ctx, &exchange)
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeChangeExchangeRate,
			sdk.NewAttribute(types.AttributeKeyExchangeId, exchanges[0].Id.String()),
			sdk.NewAttribute(types.AttributeKeyExchangeRate, rawDataDec.String()),
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
