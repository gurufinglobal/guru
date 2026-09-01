package types

// SeparationRatioScalePPM is 100% in parts per million.
const SeparationRatioScalePPM uint32 = 1_000_000

const (
	MinGasPriceScaleFactor          = "630000000000"
	MinGasPriceOraclePricePrecision = "1000000000000000000"
	MinGasPriceClampPPM             = 100_000
	MinGasPricePendingDelayCap      = 10
)

const (
	EventTypeMinGasPriceUpdateScheduled = "min_gas_price_update_scheduled"
	EventTypeMinGasPriceUpdateApplied   = "min_gas_price_update_applied"
	EventTypeMinGasPriceUpdateSkipped   = "min_gas_price_update_skipped"

	MinGasPriceUpdateReasonMissedEffectiveHeight = "missed_effective_height"

	AttributeKeySourceSymbol             = "source_symbol"
	AttributeKeySourceValue              = "source_value"
	AttributeKeySourceOracleHeight       = "source_oracle_height"
	AttributeKeySourceSubmissionInterval = "source_submission_interval"
	AttributeKeyPendingDelayBlocks       = "pending_delay_blocks"
	AttributeKeyPendingDelayCapBlocks    = "pending_delay_cap_blocks"
	AttributeKeyScheduledHeight          = "scheduled_height"
	AttributeKeyEffectiveHeight          = "effective_height"
	AttributeKeyPreviousEffectiveHeight  = "previous_effective_height"
	AttributeKeyPreviousMinGasPrice      = "previous_min_gas_price"
	AttributeKeyRawMinGasPrice           = "raw_min_gas_price"
	AttributeKeyClampedMinGasPrice       = "clamped_min_gas_price"
	AttributeKeyPendingMinGasPrice       = "pending_min_gas_price"
	AttributeKeyScaleFactor              = "scale_factor"
	AttributeKeyClampPPM                 = "clamp_ppm"
	AttributeKeyReplaced                 = "replaced"
	AttributeKeyApplyHeight              = "apply_height"
	AttributeKeyNewMinGasPrice           = "new_min_gas_price"
	AttributeKeyHeight                   = "height"
	AttributeKeyObservedHeight           = "observed_height"
	AttributeKeyNextHeight               = "next_height"
	AttributeKeyReason                   = "reason"
	AttributeKeyCurrentMinGasPrice       = "current_min_gas_price"
	AttributeKeyScheduledMinGasPrice     = "scheduled_min_gas_price"
)
