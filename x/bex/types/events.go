package types

const (
	EventTypeAdminRegistered    = "admin_registered"
	EventTypeAdminRemoved       = "admin_removed"
	EventTypeExchangeRegistered = "exchange_registered"
	EventTypeExchangeUpdated    = "exchange_updated"
	EventTypeExchangeStatus     = "exchange_status_changed"
	EventTypeExchangeDeleted    = "exchange_deleted"
	EventTypeReserveDeposited   = "reserve_deposited"
	EventTypeReserveWithdrawn   = "reserve_withdrawn"
	EventTypeFeesCollected      = "fees_collected"
	EventTypeFeesLocked         = "fees_locked"
	EventTypeFeesReleased       = "fees_released"
	EventTypeFeesDeducted       = "fees_deducted"
	EventTypeFeesWithdrawn      = "fees_withdrawn"
	EventTypeVolumeRecorded     = "volume_recorded"
	EventTypeVolumeCapExceeded  = "volume_cap_exceeded"
)

const (
	AttributeKeyAdmin          = "admin"
	AttributeKeyModerator      = "moderator"
	AttributeKeyExchangeID     = "exchange_id"
	AttributeKeyReserveAddress = "reserve_address"
	AttributeKeyRecipient      = "recipient"
	AttributeKeyAmount         = "amount"
	AttributeKeyDirection      = "direction"
	AttributeKeyRevision       = "revision"
	AttributeKeyStatus         = "status"
	AttributeKeyPreviousStatus = "previous_status"
	AttributeKeyCurrentAmount  = "current_amount"
	AttributeKeyNextAmount     = "next_amount"
	AttributeKeyCap            = "cap"
)
