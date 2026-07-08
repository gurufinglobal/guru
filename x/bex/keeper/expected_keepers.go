package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
)

type AccountKeeper interface {
	NewAccountWithAddress(context.Context, sdk.AccAddress) sdk.AccountI
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
	SetAccount(context.Context, sdk.AccountI)
}

type BankKeeper interface {
	AppendSendRestriction(restriction banktypes.SendRestrictionFn)
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

type OracleKeeper interface {
	GetLatestValue(ctx context.Context, symbol string) (*oraclev1.OracleValue, error)
}

type ConstitutionKeeper interface {
	GetModeratorAddress(ctx context.Context) (string, error)
}

type ChannelKeeper interface {
	GetChannel(ctx sdk.Context, portID, channelID string) (channeltypes.Channel, bool)
}
