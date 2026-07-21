package types

import (
	"math"

	sdkmath "cosmossdk.io/math"

	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
)

const (
	MinVolumeEpochSeconds = uint32(86400)
	MaxVolumeEpochSeconds = uint32(604800)
)

// ValidateVolumeReservation validates the persisted identity used to reverse
// one asynchronous volume charge. The amount is limited to uint256 so it has
// the same domain as quotes and volume windows.
func ValidateVolumeReservation(reservation *VolumeReservation) (sdkmath.Int, error) {
	if reservation == nil || reservation.GetExchangeId() == 0 {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation is required")
	}
	switch reservation.GetDirection() {
	case SwapDirection_SWAP_DIRECTION_A_TO_B,
		SwapDirection_SWAP_DIRECTION_B_TO_A:
	default:
		return sdkmath.Int{}, ErrInvalidRoute.Wrap("volume reservation direction is invalid")
	}
	seconds := reservation.GetEpochSeconds()
	if seconds < MinVolumeEpochSeconds || seconds > MaxVolumeEpochSeconds {
		return sdkmath.Int{}, ErrInvalidRequest.Wrapf(
			"volume reservation epoch must be between %d and %d seconds",
			MinVolumeEpochSeconds,
			MaxVolumeEpochSeconds,
		)
	}
	if reservation.GetVolumeWindowGeneration() == 0 {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation generation must be non-zero")
	}
	if reservation.GetEpochStartUnix()%uint64(seconds) != 0 {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation epoch is not aligned")
	}
	if reservation.GetEpochStartUnix() > math.MaxUint64-uint64(seconds) {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation expiry overflows uint64")
	}
	value := reservation.GetAmount()
	amount, err := uint256decimal.ParseCanonicalPositive(value)
	if err != nil {
		return sdkmath.Int{}, ErrInvalidRequest.Wrapf("volume reservation amount must be a canonical positive uint256: %v", err)
	}
	return amount, nil
}
