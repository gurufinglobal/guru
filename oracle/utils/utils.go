package utils

import (
	"fmt"
	"strconv"
	"sync/atomic"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

func EventToRequestID(event coretypes.ResultEvent) (uint64, error) {
	eventKey := oracletypes.EventTypeOracleTask + "." + oracletypes.AttributeKeyRequestID

	ids, ok := event.Events[eventKey]
	if !ok {
		return 0, fmt.Errorf("event '%s' missing request id", eventKey)
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("event '%s' has no request id", eventKey)
	}

	return strconv.ParseUint(ids[0], 10, 64)
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
