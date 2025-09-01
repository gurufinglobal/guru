package types

import (
	"fmt"
	"time"

	feemarkettypes "github.com/GPTx-global/guru-v2/x/feemarket/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
)

var (
	RegisterQuery       = "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'"
	RegisterID          = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId
	RegisterAccountList = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyAccountList

	UpdateQuery = "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'"
	UpdateID    = oracletypes.EventTypeUpdateOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId

	CompleteQuery = fmt.Sprintf("tm.event='NewBlock' AND %s.%s EXISTS", oracletypes.EventTypeCompleteOracleDataSet, oracletypes.AttributeKeyRequestId)
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
