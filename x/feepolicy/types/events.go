package types

// cex module event types
const (
	// event types
	EventTypeRegisterDiscounts = "register_discounts"
	EventTypeRemoveDiscounts   = "remove_discounts"
	EventTypeChangeModerator   = "change_moderator_address"

	// event attributes
	AttributeKeyAddress      = "address"
	AttributeKeyModerator    = "moderator"
	AttributeKeyModule       = "module"
	AttributeKeyMsgType      = "msg_type"
	AttributeKeyDiscountType = "discount_type"
	AttributeKeyAmount       = "amount"
)
