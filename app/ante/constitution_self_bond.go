package ante

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type ConstitutionSelfBondKeeper interface {
	ValidateTxSelfBondConstraints(ctx context.Context, tx sdk.Tx, accountCodec address.Codec) error
}

func WrapAnteHandlerWithConstitutionSelfBondCheck(
	next sdk.AnteHandler,
	keeper ConstitutionSelfBondKeeper,
	accountCodec address.Codec,
) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if err := keeper.ValidateTxSelfBondConstraints(ctx, tx, accountCodec); err != nil {
			return ctx, err
		}

		return next(ctx, tx, simulate)
	}
}
