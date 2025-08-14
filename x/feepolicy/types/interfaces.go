package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ModuleKeeper defines the interface for the custom keeper
type ModuleKeeper interface {
	CheckDiscount(ctx sdk.Context, discounts Discount, msgs []sdk.Msg) bool
}
