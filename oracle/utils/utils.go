package utils

import (
	"fmt"
	"strconv"

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
