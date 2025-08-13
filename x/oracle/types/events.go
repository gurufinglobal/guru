package types

// Oracle module event type constants
const (
	// EventTypeRegisterOracleRequestDoc defines the event type for registering oracle request document
	EventTypeRegisterOracleRequestDoc = "register_oracle_request_doc"

	// EventTypeUpdateOracleRequestDoc defines the event type for updating oracle request document
	EventTypeUpdateOracleRequestDoc = "update_oracle_request_doc"

	// EventTypeCompleteOracleDataSet defines the event type for complete oracle data set
	EventTypeCompleteOracleDataSet = "complete_oracle_data_set"

	// EventTypeUpdateModeratorAddress defines the event type for updating moderator address
	EventTypeUpdateModeratorAddress = "update_moderator_address"

	// EventTypeSubmitOracleData defines the event type for submitting oracle data
	EventTypeSubmitOracleData = "submit_oracle_data"
)

// Event attribute keys
const (
	AttributeKeyRequestId        = "request_id"
	AttributeKeyOracleType       = "oracle_type"
	AttributeKeyName             = "name"
	AttributeKeyDescription      = "description"
	AttributeKeyPeriod           = "period"
	AttributeKeyAccountList      = "account_list"
	AttributeKeyEndpoints        = "endpoints"
	AttributeKeyAggregateRule    = "aggregate_rule"
	AttributeKeyStatus           = "status"
	AttributeKeyCreator          = "creator"
	AttributeKeyModeratorAddress = "moderator_address"
	AttributeKeyNonce            = "nonce"
	AttributeKeyFromAddress      = "from_address"
	AttributeKeyRawData          = "raw_data"
	AttributeKeyAggregationRule  = "aggregation_rule"
	AttributeKeyQuorum           = "quorum"
)

const (
	AttributeKeyOracleDataNone = "oracle_data_nonce"
)
