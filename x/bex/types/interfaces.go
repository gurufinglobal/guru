package types

import (
	context "context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper defines the contract required for account APIs.
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
}

// BankKeeper defines the contract needed to be fulfilled for banking and supply
// dependencies.
type BankKeeper interface {
	// GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	// MintCoins(ctx sdk.Context, name string, amt sdk.Coins) error
	// BurnCoins(ctx sdk.Context, name string, amt sdk.Coins) error
	// SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	// SendCoinsFromAccountToModule(ctx sdk.Context, senderAddress sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
}
