package types

import (
	"context"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
)

// TransferKeeper defines the subset of IBC transfer keeper methods required by
// Packet Forward Middleware (PFM) plus feeproxy's Transfer interception.
//
// This interface intentionally mirrors PFM's expected TransferKeeper so that
// feeproxy.Keeper can be injected wherever PFM expects a TransferKeeper.
type TransferKeeper interface {
	Transfer(ctx context.Context, msg *transfertypes.MsgTransfer) (*transfertypes.MsgTransferResponse, error)

	GetDenom(ctx sdk.Context, denomHash cmtbytes.HexBytes) (transfertypes.Denom, bool)
	GetTotalEscrowForDenom(ctx sdk.Context, denom string) sdk.Coin
	SetTotalEscrowForDenom(ctx sdk.Context, coin sdk.Coin)
	DenomPathFromHash(ctx sdk.Context, ibcDenom string) (string, error)

	// Only used for PFM migrations but required by the interface.
	GetPort(ctx sdk.Context) string
}

type BankKeeper interface {
	// Lock: move coins into module escrow account.
	SendCoinsFromAccountToModule(ctx context.Context, fromAddr sdk.AccAddress, toModule string, amt sdk.Coins) error

	// Refund/top-up: move coins from module escrow to a regular account (e.g. transfer escrow address).
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

