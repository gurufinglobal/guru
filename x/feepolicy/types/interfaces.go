package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ModuleKeeper optionally applies module-specific eligibility checks after an
// exact feepolicy match. A veto does not fall back to the global policy.
type ModuleKeeper interface {
	CheckDiscount(context.Context, Discount, []sdk.Msg) bool
}
