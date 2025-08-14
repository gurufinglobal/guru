package types

// cex module event types
const (
	// event types
	EventTypeRegisterDiscounts = ModuleName + "_register_discounts"
	EventTypeRemoveDiscounts   = ModuleName + "_remove_discounts"
	EventTypeChangeModerator   = ModuleName + "_change_moderator_address"

	// event attributes
	AttributeKeyAddress   = "address"
	AttributeKeyModerator = "moderator"
	AttributeKeyModule    = "module"
	AttributeKeyMsgType   = "msg_type"
	AttributeKeyAmount    = "amount"
)
