package utils

import (
	"fmt"
	"strconv"
	"sync/atomic"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	feemarkettypes "github.com/gurufinglobal/guru/v2/x/feemarket/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func EventToRequestIDs(event coretypes.ResultEvent) ([]uint64, error) {
	eventKey := oracletypes.EventTypeOracleTask + "." + oracletypes.AttributeKeyRequestID

	rawIDs, ok := event.Events[eventKey]
	if !ok {
		if _, ok := event.Events[oracletypes.EventTypeUpdateMinGasPrice+"."+feemarkettypes.AttributeKeyMinGasPrice]; ok {
			return []uint64{0}, nil
		}
		return nil, fmt.Errorf("event '%s' missing request id", eventKey)
	}
	if len(rawIDs) == 0 {
		return nil, fmt.Errorf("event '%s' has no request id", eventKey)
	}

	ids := make([]uint64, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		id, err := strconv.ParseUint(rawID, 10, 64)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, nil
}

func TrackFailStreak() (count func() int64, reset func(), inc func()) {
	failStreak := int64(0)

	count = func() int64 {
		return atomic.LoadInt64(&failStreak)
	}

	reset = func() {
		atomic.StoreInt64(&failStreak, 0)
	}

	inc = func() {
		atomic.AddInt64(&failStreak, 1)
	}

	return count, reset, inc
}
