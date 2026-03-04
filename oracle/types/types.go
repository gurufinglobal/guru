package types

import (
	"time"

	feemarkettypes "github.com/gurufinglobal/guru/v2/x/feemarket/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

const (
	ChannelBufferSize = 32

	SubscriberName = "oracle_daemon_v2"

	OracleTaskIDQuery = "tm.event='NewBlock' AND " + oracletypes.EventTypeOracleTask + "." + oracletypes.AttributeKeyRequestID + " EXISTS"
	MinGasPriceQuery = "tm.event='NewBlock' AND " + oracletypes.EventTypeUpdateMinGasPrice + "." + feemarkettypes.AttributeKeyMinGasPrice + " EXISTS"

	HealthCheckInterval = 30 * time.Second
)

type OracleTask struct {
	Id       uint64
	Category int32
	Symbol   string
	Nonce    uint64
}
