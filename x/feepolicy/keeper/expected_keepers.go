package keeper

import "context"

// ConstitutionKeeper is the single source of truth for the chain moderator.
// The update method exists only so the legacy feepolicy ChangeModerator wire
// API can proxy to Constitution instead of creating a second state owner.
type ConstitutionKeeper interface {
	GetModeratorAddress(ctx context.Context) (string, error)
	UpdateModeratorAddress(ctx context.Context, moderatorAddress string) error
}
