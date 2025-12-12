package types

import (
	time "time"

	errorsmod "cosmossdk.io/errors"
)

func ValidateRatemeter(ratemeter *Ratemeter) error {
	if ratemeter.RequestCountLimit <= 0 {
		return errorsmod.Wrapf(ErrInvalidRequestCountLimit, "request_count_limit should be greater than zero")
	}

	if ratemeter.RequestPeriod <= 0 {
		return errorsmod.Wrapf(ErrInvalidRequestPeriod, "request_period must be greater than zero")
	}

	// Minimum = 1 hour
	if ratemeter.RequestPeriod < time.Hour {
		return errorsmod.Wrapf(ErrInvalidRequestPeriod, "minimum request_period is 1h")
	}

	// Must align to whole hours
	if ratemeter.RequestPeriod%time.Hour != 0 {
		return errorsmod.Wrapf(ErrInvalidRequestPeriod, "request_period must be a whole multiple of 1h")
	}

	return nil
}

func DefaultRatemeter() Ratemeter {
	return Ratemeter{
		RequestCountLimit: 5,
		RequestPeriod:     time.Hour * 24,
	}
}
