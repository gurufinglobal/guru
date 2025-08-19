// Package types defines common data structures used across Oracle daemon components
// Contains job definitions, results, and event type constants
package types

import (
	"time"

	feemarkettypes "github.com/GPTx-global/guru-v2/x/feemarket/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// SubscribeType defines the union type for Oracle event subscriptions
// Allows type-safe handling of different event types from the blockchain
type SubscribeType interface {
	oracletypes.OracleRequestDoc | coretypes.ResultEvent
}

// Event type constants for blockchain event filtering and parsing
// These map to specific Oracle module events emitted by the blockchain
var (
	// Oracle request registration events
	RegisterID          = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId
	RegisterAccountList = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyAccountList

	// Oracle request update events
	UpdateID = oracletypes.EventTypeUpdateOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId

	// Oracle completion events
	CompleteID    = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyRequestId
	CompleteNonce = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyNonce

	// Gas price update events
	MinGasPrice = feemarkettypes.EventTypeChangeMinGasPrice + "." + feemarkettypes.AttributeKeyMinGasPrice
)

// OracleJob represents a data fetching task for a specific Oracle request
// Contains all information needed to fetch external data and track execution state
type OracleJob struct {
	ID     uint64                    // Unique identifier for the Oracle request
	URL    string                    // External API endpoint to fetch data from
	Path   string                    // JSON path to extract specific data from response
	Nonce  uint64                    // Current nonce for tracking job execution order
	Delay  time.Duration             // Interval between job executions
	Status oracletypes.RequestStatus // Current status of the Oracle request
}

// OracleJobResult contains the processed result of an Oracle job execution
// This data will be submitted to the blockchain as Oracle data
type OracleJobResult struct {
	ID    uint64 // Oracle request ID this result belongs to
	Data  string // Processed data extracted from external source
	Nonce uint64 // Nonce for this specific result submission
}
