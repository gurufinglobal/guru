package keeper

import "context"

type ConstitutionKeeper interface {
	GetModeratorAddress(ctx context.Context) (string, error)
}
