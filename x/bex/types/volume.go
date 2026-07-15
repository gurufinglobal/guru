package types

import (
	"math"
	"strings"

	sdkmath "cosmossdk.io/math"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
)

const (
	MinVolumeEpochSeconds = uint32(86400)
	MaxVolumeEpochSeconds = uint32(604800)
)

// ValidateVolumeReservation validates the persisted identity used to reverse
// one asynchronous volume charge. The amount is limited to uint256 so it has
// the same domain as quotes and volume windows.
func ValidateVolumeReservation(reservation *bexv1.VolumeReservation) (sdkmath.Int, error) {
	if reservation == nil || reservation.GetExchangeId() == 0 {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation is required")
	}
	switch reservation.GetDirection() {
	case bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		bexv1.SwapDirection_SWAP_DIRECTION_B_TO_A:
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
	if value == "" || value != strings.TrimSpace(value) {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation amount must be a canonical positive integer")
	}
	amount, ok := sdkmath.NewIntFromString(value)
	if !ok || !amount.IsPositive() || amount.String() != value || amount.BigInt().BitLen() > 256 {
		return sdkmath.Int{}, ErrInvalidRequest.Wrap("volume reservation amount must be a canonical positive uint256")
	}
	return amount, nil
}
