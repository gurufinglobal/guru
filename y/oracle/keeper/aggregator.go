package keeper

import (
	"fmt"
	"math/big"
	"sort"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

// aggregateReports aggregates raw data values using median for robustness.
func aggregateReports(reports []types.OracleReport) (string, error) {
	if len(reports) == 0 {
		return "", fmt.Errorf("no reports to aggregate")
	}

	values := make([]*big.Float, len(reports))
	for i, r := range reports {
		values[i] = new(big.Float)
		if _, ok := values[i].SetString(r.RawData); !ok {
			return "", fmt.Errorf("invalid raw data: %q", r.RawData)
		}
	}

	// Use standard sort.Slice
	sort.Slice(values, func(i, j int) bool {
		return values[i].Cmp(values[j]) < 0
	})

	mid := len(values) / 2
	if len(values)%2 == 0 {
		sum := new(big.Float).Add(values[mid-1], values[mid])
		sum.Quo(sum, big.NewFloat(2))
		return sum.Text('f', -1), nil
	}
	return values[mid].Text('f', -1), nil
}

// calculateQuorumThreshold calculates the minimum number of reports required based on quorum_ratio.
// calculateQuorumThreshold calculates the minimum number of reports required based on quorum_ratio.
func (k Keeper) calculateQuorumThreshold(total uint64, ratio math.LegacyDec) uint64 {
	if total == 0 {
		return 0
	}

	totalDec := math.LegacyNewDec(int64(total))
	thresholdDec := totalDec.Mul(ratio)

	// Ceiling: if there's a fractional part, round up
	threshold := thresholdDec.TruncateInt64()
	if thresholdDec.GT(math.LegacyNewDec(threshold)) {
		threshold++
	}

	return uint64(threshold)
}

// tryAggregate attempts to aggregate reports for a specific request and nonce.
func (k Keeper) tryAggregate(ctx sdk.Context, req types.OracleRequest, nonce, totalProviders uint64, params types.Params) (*types.OracleResult, bool, error) {
	if nonce == 0 {
		// 보호: 유효한 기간이 설정되지 않은 경우 건너뛴다.
		return nil, false, nil
	}

	if _, exists := k.GetResult(ctx, req.Id, nonce); exists {
		k.Logger(ctx).Debug("result already exists", "request_id", req.Id, "nonce", nonce)
		return nil, false, nil
	}

	if totalProviders == 0 {
		k.Logger(ctx).Debug("no whitelisted providers", "request_id", req.Id)
		return nil, false, nil
	}

	threshold := k.calculateQuorumThreshold(totalProviders, params.QuorumRatio)

	// Optimization: Check count before loading all reports
	reportCount := k.GetReportCount(ctx, req.Id, nonce)

	k.Logger(ctx).Debug("checking quorum",
		"request_id", req.Id,
		"nonce", nonce,
		"reports", reportCount,
		"threshold", threshold,
		"total_providers", totalProviders,
	)

	if reportCount < threshold {
		return nil, false, nil
	}

	// Now load reports for aggregation
	var reports []types.OracleReport
	k.IterateReports(ctx, req.Id, nonce, func(report types.OracleReport) bool {
		reports = append(reports, report)
		return false
	})

	agg, err := aggregateReports(reports)
	if err != nil {
		k.Logger(ctx).Error("aggregation failed", "request_id", req.Id, "nonce", nonce, "error", err)
		return nil, false, err
	}

	result := types.OracleResult{
		RequestId:        req.Id,
		AggregatedData:   agg,
		AggregatedHeight: uint64(ctx.BlockHeight()),
		AggregatedTime:   uint64(ctx.BlockTime().Unix()),
		Nonce:            nonce,
	}

	k.SetResult(ctx, result)

	k.Logger(ctx).Info("aggregation completed",
		"request_id", req.Id,
		"nonce", nonce,
		"aggregated_data", agg,
		"reports_count", reportCount,
	)

	return &result, true, nil
}

// ProcessOracleReportAggregation scans active requests and aggregates when quorum is met.
// It is called from EndBlocker to batch emit events.
func (k Keeper) ProcessOracleReportAggregation(ctx sdk.Context) {
	params := k.GetParams(ctx)
	if !params.Enable {
		return
	}
	// Fetch whitelist count once to avoid N+1 queries
	totalProviders := k.GetWhitelistCount(ctx)

	k.IterateRequests(ctx, func(req types.OracleRequest) bool {
		if req.Status != types.Status_STATUS_ACTIVE {
			return false
		}

		result, aggregated, err := k.tryAggregate(ctx, req, req.Nonce, totalProviders, params)
		if err != nil {
			k.Logger(ctx).Error("aggregation error", "request_id", req.Id, "error", err)
			return false
		}
		if !aggregated {
			// quorum not met (normal case) or aggregation skipped (e.g. no providers / result already exists)
			return false
		}
		if k.hooks != nil && result != nil {
			k.hooks.AfterOracleAggregation(ctx, req, *result)
		}

		return false
	})
}
