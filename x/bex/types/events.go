package types

// bex module event types
const (
	// event types
	EventTypeRegisterAdmin      = "register_admin"
	EventTypeRemoveAdmin        = "remove_admin"
	EventTypeRegisterExchange   = "register_exchange"
	EventTypeUpdateExchange     = "update_exchange"
	EventTypeUpdateRatemeter    = "update_ratemeter"
	EventTypeChangeModerator    = "change_moderator"
	EventTypeWithdrawFees       = "withdraw_fees"
	EventTypeChangeExchangeRate = "change_exchange_rate"

	// event attributes
	AttributeKeyAddress           = "address"
	AttributeKeyModerator         = "moderator"
	AttributeKeyAdmin             = "admin"
	AttributeKeyExchangeID        = "exchange_id"
	AttributeKeyKey               = "key"
	AttributeKeyValue             = "value"
	AttributeKeyRequestCountLimit = "request_count_limit"
	AttributeKeyRequestPeriod     = "request_period"
	AttributeKeyWithdrawAddress   = "withdraw_address"
	AttributeKeyAmount            = "amount"
	AttributeKeyExchangeRate      = "rate"
)
