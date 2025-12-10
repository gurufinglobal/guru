package types

const (
	// EventTypeOracleTask is the single event type for oracle daemon to listen.
	// Emitted when: 1) new request registered, 2) aggregation completed + period blocks.
	EventTypeOracleTask = "oracle_task"

	// Legacy event types for internal logging (not for daemon)
	EventTypeRegisterRequest = "register_oracle_request"
	EventTypeUpdateRequest   = "update_oracle_request"
	EventTypeSubmitReport    = "submit_oracle_report"
	EventTypeAggregateResult = "aggregate_oracle_result"
	EventTypeUpdateModerator = "update_moderator_address"
)

const (
	// AttributeKeyRequestID is the attribute key for the request ID.
	AttributeKeyRequestID = "request_id"
	// AttributeKeyCategory is the attribute key for the category.
	AttributeKeyCategory = "category"
	// AttributeKeySymbol is the attribute key for the symbol.
	AttributeKeySymbol = "symbol"
	// AttributeKeyCount is the attribute key for the count.
	AttributeKeyCount = "count"
	// AttributeKeyPeriod is the attribute key for the period.
	AttributeKeyPeriod = "period"
	// AttributeKeyStatus is the attribute key for the status.
	AttributeKeyStatus = "status"
	// AttributeKeyNonce is the attribute key for the nonce.
	AttributeKeyNonce = "nonce"
	// AttributeKeyProvider is the attribute key for the provider.
	AttributeKeyProvider = "provider"
	// AttributeKeyRawData is the attribute key for the raw data.
	AttributeKeyRawData = "raw_data"
	// AttributeKeyAggregatedData is the attribute key for the aggregated data.
	AttributeKeyAggregatedData = "aggregated_data"
	// AttributeKeyBlockHeight is the attribute key for the block height.
	AttributeKeyBlockHeight = "block_height"
	// AttributeKeyBlockTime is the attribute key for the block time.
	AttributeKeyBlockTime = "block_time"
	// AttributeKeyModerator is the attribute key for the moderator address.
	AttributeKeyModerator = "moderator_address"
)
