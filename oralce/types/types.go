package types

import (
	"time"

	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

// Feemarket event constants (previously from x/feemarket/types/events.go)
const (
	EventTypeChangeMinGasPrice = "change_min_gas_price"
	AttributeKeyMinGasPrice    = "min_gas_price"
)

var (
	RegisterID          = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyRequestID
	RegisterAccountList = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyAccountList

	UpdateID = oracletypes.EventTypeUpdateOracleRequestDoc + "." + oracletypes.AttributeKeyRequestID

	CompleteID    = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyRequestID
	CompleteNonce = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyNonce
	CompleteTime  = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyBlockTime

	MinGasPrice = EventTypeChangeMinGasPrice + "." + AttributeKeyMinGasPrice
)

type OracleJob struct {
	ID     uint64
	URL    string
	Path   string
	Nonce  uint64
	Delay  time.Duration
	Period time.Duration
	Status oracletypes.RequestStatus
}

type OracleJobResult struct {
	ID    uint64
	Data  string
	Nonce uint64
}
