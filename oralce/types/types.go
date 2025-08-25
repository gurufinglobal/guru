package types

import (
	"time"

	feemarkettypes "github.com/GPTx-global/guru-v2/x/feemarket/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
)

var (
	RegisterID          = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId
	RegisterAccountList = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyAccountList

	UpdateID = oracletypes.EventTypeUpdateOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId

	CompleteID    = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyRequestId
	CompleteNonce = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyNonce
	CompleteTime  = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyBlockTime

	MinGasPrice = feemarkettypes.EventTypeChangeMinGasPrice + "." + feemarkettypes.AttributeKeyMinGasPrice
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
