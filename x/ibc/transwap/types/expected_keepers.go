package types

import (
	"context"

	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	bexv1 "github.com/gurufinglobal/guru/v3/x/bex/types"
)

// AccountKeeper defines the contract required for account APIs.
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, name string) sdk.ModuleAccountI
}

// BankKeeper defines the expected bank keeper
type BankKeeper interface {
	SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BlockedAddr(addr sdk.AccAddress) bool
	IsSendEnabledCoins(ctx context.Context, coins ...sdk.Coin) error
	HasDenomMetaData(ctx context.Context, denom string) bool
	SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata)
	SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// ChannelKeeper defines the expected IBC channel keeper
type ChannelKeeper interface {
	GetChannel(ctx sdk.Context, srcPort, srcChan string) (channel channeltypes.Channel, found bool)
	GetNextSequenceSend(ctx sdk.Context, portID, channelID string) (uint64, bool)
	GetAllChannelsWithPortPrefix(ctx sdk.Context, portPrefix string) []channeltypes.IdentifiedChannel
	HasChannel(ctx sdk.Context, portID, channelID string) bool
	GetPacketCommitment(ctx sdk.Context, portID, channelID string, sequence uint64) []byte
}

type BexKeeper interface {
	ValidateSwapInput(ctx context.Context, exchangeID uint64, inputDenom, localInputDenom string) (bexv1.SwapDirection, error)
	QuoteSwap(ctx context.Context, req *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error)
	ReceiveToReserve(ctx context.Context, exchangeID uint64, fromAddr sdk.AccAddress, amount sdk.Coins) error
	SendSwapOutputFromReserve(ctx context.Context, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coin) error
	SendRefundFromReserve(ctx context.Context, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coin) error
	ClaimRefundFromReserve(ctx context.Context, exchangeID uint64, recipient sdk.AccAddress, amount sdk.Coin) error
	ReserveVolumeWindow(ctx context.Context, exchangeID uint64, direction bexv1.SwapDirection, amountOut math.Int) (*bexv1.VolumeReservation, error)
	ReleaseVolumeWindow(ctx context.Context, reservation *bexv1.VolumeReservation) error
	CollectFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error
	LockExchangeFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error
	ReleaseExchangeFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error
	RefundLockedFee(ctx context.Context, exchangeID uint64, fee sdk.Coin) error
	AddPendingLiability(ctx context.Context, exchangeID uint64, liability sdk.Coin) error
	ReleasePendingLiability(ctx context.Context, exchangeID uint64, liability sdk.Coin) error
	GetPendingLiabilities(ctx context.Context, exchangeID uint64) (sdk.Coins, error)
	GetLockedFees(ctx context.Context, exchangeID uint64) (sdk.Coins, error)
	GetRefundAccountingExchangeIDs(ctx context.Context) ([]uint64, error)
	GetReserveAddress(ctx context.Context, exchangeID uint64) sdk.AccAddress
}

// MessageRouter ADR 031 request type routing
// https://github.com/cosmos/cosmos-sdk/blob/main/docs/architecture/adr-031-msg-service.md
type MessageRouter interface {
	Handler(msg sdk.Msg) baseapp.MsgServiceHandler
}

// ConnectionKeeper defines the expected IBC connection keeper
type ConnectionKeeper interface {
	GetConnection(ctx sdk.Context, connectionID string) (connection connectiontypes.ConnectionEnd, found bool)
}

// ClientKeeper exposes the latest trusted destination consensus timestamp used
// to anchor fresh refund packet timeouts.
type ClientKeeper interface {
	GetLatestClientConsensusState(ctx sdk.Context, clientID string) (ibcexported.ConsensusState, bool)
}

// ParamSubspace defines the expected Subspace interface for module parameters.
type ParamSubspace interface {
	GetParamSet(ctx sdk.Context, ps paramtypes.ParamSet)
}
