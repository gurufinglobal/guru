package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type BankKeeper interface {
	BlockedAddr(addr sdk.AccAddress) bool
	GetBlockedAddresses() map[string]bool
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

type FeeMarketKeeper interface {
	GetMinGasPrice(ctx context.Context) sdkmath.LegacyDec
	SetMinGasPrice(ctx context.Context, minGasPrice sdkmath.LegacyDec) error
}
