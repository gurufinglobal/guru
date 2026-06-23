package ante

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SelfBondConstraintKeeper interface {
	ValidateTxSelfBondConstraints(ctx context.Context, tx sdk.Tx) error
}

func WrapAnteHandlerWithSelfBondCheck(
	next sdk.AnteHandler,
	keeper SelfBondConstraintKeeper,
) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if err := keeper.ValidateTxSelfBondConstraints(ctx, tx); err != nil {
			return ctx, err
		}

		return next(ctx, tx, simulate)
	}
}
