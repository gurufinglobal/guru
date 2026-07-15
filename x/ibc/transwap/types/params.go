package types

import (
	"time"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
)

const (
	DefaultMaxRefundRetries uint32 = 3
	// The governance value remains configurable up to the existing hard bound.
	// Per-block work is bounded independently by MaxRefundRetryDispatchesPerBlock.
	MaximumMaxRefundRetries          uint32 = 100
	DefaultRefundTimeoutWindow              = 5 * time.Minute
	DefaultMinRelaySafetyMargin             = 30 * time.Second
	MaximumRefundTimeoutWindow              = 24 * time.Hour
	MaxRefundRetryDispatchesPerBlock        = 10
)

func DefaultParams() *transwapv1.Params {
	return &transwapv1.Params{
		MaxRefundRetries:     DefaultMaxRefundRetries,
		RefundTimeoutWindow:  uint64(DefaultRefundTimeoutWindow),
		MinRelaySafetyMargin: uint64(DefaultMinRelaySafetyMargin),
	}
}

func ValidateParams(params *transwapv1.Params) error {
	if params == nil {
		return ErrInvalidParams.Wrap("params cannot be nil")
	}
	if params.GetMaxRefundRetries() == 0 || params.GetMaxRefundRetries() > MaximumMaxRefundRetries {
		return ErrInvalidParams.Wrapf(
			"max_refund_retries must be between 1 and %d",
			MaximumMaxRefundRetries,
		)
	}
	window := params.GetRefundTimeoutWindow()
	if window == 0 || window > uint64(MaximumRefundTimeoutWindow) {
		return ErrInvalidParams.Wrapf(
			"refund_timeout_window must be between 1ns and %s",
			MaximumRefundTimeoutWindow,
		)
	}
	if params.GetMinRelaySafetyMargin() >= window {
		return ErrInvalidParams.Wrap("min_relay_safety_margin must be less than refund_timeout_window")
	}
	return nil
}
