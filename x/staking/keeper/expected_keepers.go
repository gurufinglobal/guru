package keeper

import (
	"context"

	"cosmossdk.io/math"
)

type MinValidatorBondSource interface {
	GetMinValidatorBondAmount(ctx context.Context) (math.Int, error)
}
