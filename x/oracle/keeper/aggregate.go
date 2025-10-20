package keeper

import (
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/gurufinglobal/guru/v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ProcessOracleDataSetAggregation processes oracle data set aggregation for all enabled requests
// It performs the following steps:
// 1. Retrieves all registered OracleRequestDocs
// 2. For each enabled document:
//   - Gets submit data sets for the next nonce
//   - Checks if quorum is met
//   - Aggregates data based on the rule
//   - Stores the result and emits events
func (k Keeper) ProcessOracleDataSetAggregation(ctx sdk.Context) {
	// Get all registered OracleRequestDocs
	requestDocs := k.GetOracleRequestDocs(ctx)
	if len(requestDocs) == 0 {
		k.Logger(ctx).Info("no oracle request docs found")
		return
	}

	// Process each OracleRequestDoc
	for _, doc := range requestDocs {
		if doc.Status != types.RequestStatus_REQUEST_STATUS_ENABLED {
			continue
		}

		// Get submit data sets for current request_id and next nonce
		nextNonce := doc.Nonce + 1
		submitDatas, err := k.GetSubmitDatas(ctx, doc.RequestId, nextNonce)
		if err != nil {
			k.Logger(ctx).Error("failed to get submit datas",
				"request_id", doc.RequestId,
				"nonce", nextNonce,
				"error", err)
			continue
		}

		// Check if we have enough submissions (quorum)
		if uint32(len(submitDatas)) < doc.Quorum {
			k.Logger(ctx).Info(fmt.Sprintf("insufficient submissions for request_id %d, nonce %d: got %d, need %d",
				doc.RequestId, nextNonce, len(submitDatas), doc.Quorum))
			continue
		}

		// Aggregate data based on AggregationRule
		aggregatedValue, err := k.AggregateData(ctx, doc.AggregationRule, submitDatas)
		if err != nil {
			k.Logger(ctx).Error(fmt.Sprintf("failed to aggregate data for request_id %d: %v",
				doc.RequestId, err))
			continue
		}

		// Create and store DataSet
		dataSet := types.DataSet{
			RequestId:   doc.RequestId,
			Nonce:       nextNonce,
			BlockHeight: uint64(ctx.BlockHeight()),
			BlockTime:   uint64(ctx.BlockTime().Unix()),
			RawData:     aggregatedValue,
		}

		// Store the data set
		k.SetDataSet(ctx, dataSet)

		// After storing the DataSet, the AfterOracleEnd hook is called. This is only performed when the OracleType is ORACLE_TYPE_MIN_GAS_PRICE.
		if doc.OracleType == types.OracleType_ORACLE_TYPE_MIN_GAS_PRICE {
			k.hooks.AfterOracleEnd(ctx, dataSet)
		}

		// Increment nonce
		doc.Nonce = nextNonce
		k.SetOracleRequestDoc(ctx, *doc)

		// Emit event
		ctx.EventManager().EmitEvents(
			sdk.Events{
				sdk.NewEvent(
					types.EventTypeCompleteOracleDataSet,
					sdk.NewAttribute(types.AttributeKeyRequestId, fmt.Sprintf("%d", doc.RequestId)),
					sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprintf("%d", nextNonce)),
					sdk.NewAttribute(types.AttributeKeyRawData, aggregatedValue),
					sdk.NewAttribute(types.AttributeKeyBlockHeight, fmt.Sprintf("%d", ctx.BlockHeight())),
					sdk.NewAttribute(types.AttributeKeyBlockTime, fmt.Sprintf("%d", ctx.BlockTime().Unix())),
				),
			},
		)

		k.Logger(ctx).Info(fmt.Sprintf("successfully processed oracle request %d with nonce %d",
			doc.RequestId, nextNonce))
	}
}

// aggregateData aggregates the submitted data based on the aggregation rule
func (k Keeper) AggregateData(ctx sdk.Context, rule types.AggregationRule, submitDatas []*types.SubmitDataSet) (string, error) {
	switch rule {
	case types.AggregationRule_AGGREGATION_RULE_AVG:
		return k.calculateAverage(submitDatas)
	case types.AggregationRule_AGGREGATION_RULE_MIN:
		return k.calculateMin(submitDatas)
	case types.AggregationRule_AGGREGATION_RULE_MAX:
		return k.calculateMax(submitDatas)
	case types.AggregationRule_AGGREGATION_RULE_MEDIAN:
		return k.calculateMedian(ctx, submitDatas)
	default:
		return "", fmt.Errorf("unsupported aggregation rule: %s", rule)
	}
}

func (k Keeper) calculateMax(submitDatas []*types.SubmitDataSet) (string, error) {
	if len(submitDatas) == 0 {
		return "", fmt.Errorf("no data to calculate max")
	}

	max := new(big.Float)
	for _, data := range submitDatas {
		value := new(big.Float)
		if _, ok := value.SetString(data.RawData); !ok {
			return "", fmt.Errorf("invalid decimal number in raw data: %q", data.RawData)
		}
		if value.Cmp(max) > 0 {
			max = value
		}
	}
	return max.Text('f', -1), nil
}

func (k Keeper) calculateMin(submitDatas []*types.SubmitDataSet) (string, error) {
	if len(submitDatas) == 0 {
		return "", fmt.Errorf("no data to calculate min")
	}

	min := new(big.Float).SetFloat64(math.MaxFloat64)
	for _, data := range submitDatas {
		value := new(big.Float)
		if _, ok := value.SetString(data.RawData); !ok {
			return "", fmt.Errorf("invalid decimal number in raw data: %q", data.RawData)
		}
		if value.Cmp(min) < 0 {
			min = value
		}
	}
	return min.Text('f', -1), nil
}

// calculateAverage calculates the average of all submitted values
func (k Keeper) calculateAverage(submitDatas []*types.SubmitDataSet) (string, error) {
	if len(submitDatas) == 0 {
		return "", fmt.Errorf("no data to average")
	}

	sum := new(big.Float)
	for _, data := range submitDatas {
		value := new(big.Float)
		if _, ok := value.SetString(data.RawData); !ok {
			return "", fmt.Errorf("invalid decimal number in raw data: %q", data.RawData)
		}
		sum.Add(sum, value)
	}

	avg := new(big.Float).Quo(sum, new(big.Float).SetInt64(int64(len(submitDatas))))
	return avg.Text('f', -1), nil
}

// calculateMedian calculates the median of all submitted values
func (k Keeper) calculateMedian(ctx sdk.Context, submitDatas []*types.SubmitDataSet) (string, error) {
	if len(submitDatas) == 0 {
		return "", fmt.Errorf("no data to calculate median")
	}

	// Get oracle parameters for safety limits
	params := k.GetParams(ctx)

	// Safety check: prevent DoS attacks with too many submissions
	// Since each account can only submit once, max submissions = max account list size
	if uint64(len(submitDatas)) > params.MaxAccountListSize {
		k.Logger(ctx).Error("too many submissions for median calculation",
			"count", len(submitDatas),
			"max_allowed", params.MaxAccountListSize)
		return "", fmt.Errorf("too many submissions: %d, maximum allowed: %d", len(submitDatas), params.MaxAccountListSize)
	}

	values := make([]*big.Float, len(submitDatas))
	for i, data := range submitDatas {
		values[i] = new(big.Float)
		if _, ok := values[i].SetString(data.RawData); !ok {
			return "", fmt.Errorf("invalid decimal number in raw data: %q", data.RawData)
		}
	}

	// Use efficient O(n log n) sorting instead of O(n²) bubble sort
	sort.Slice(values, func(i, j int) bool {
		return values[i].Cmp(values[j]) < 0
	})

	// Calculate median
	mid := len(values) / 2
	if len(values)%2 == 0 {
		// Even number of values, average the middle two
		median := new(big.Float).Add(values[mid-1], values[mid])
		median.Quo(median, new(big.Float).SetInt64(2))
		return median.Text('f', -1), nil
	}
	// Odd number of values, return the middle one
	return values[mid].Text('f', -1), nil
}
