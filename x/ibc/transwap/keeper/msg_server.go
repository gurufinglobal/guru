package keeper

import (
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/internal/events"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func (k Keeper) transferV1Packet(ctx sdk.Context, sourceChannel string, token *transwapv1.Token, timeoutTimestamp uint64, packetData *transwapv1.FungibleTokenPacketData) (uint64, error) {
	if err := k.SendTransfer(ctx, types.PortID, sourceChannel, token, sdk.MustAccAddressFromBech32(packetData.Sender)); err != nil {
		return 0, err
	}

	packetDataBytes := types.FungibleTokenPacketDataBytes(packetData)
	ibcHeight := clienttypes.ZeroHeight()
	sequence, err := k.ics4Wrapper.SendPacket(ctx, types.PortID, sourceChannel, ibcHeight, timeoutTimestamp, packetDataBytes)
	if err != nil {
		return 0, err
	}

	events.EmitTransferEvent(ctx, packetData.Sender, packetData.Receiver, token, packetData.Memo)

	return sequence, nil
}

func (k Keeper) transferV1PacketFromReserve(
	ctx sdk.Context,
	exchangeID uint64,
	sourceChannel string,
	token *transwapv1.Token,
	timeoutTimestamp uint64,
	packetData *transwapv1.FungibleTokenPacketData,
) (uint64, error) {
	if err := k.sendSwapOutputFromReserve(ctx, exchangeID, sourceChannel, token); err != nil {
		return 0, err
	}

	sequence, err := k.ics4Wrapper.SendPacket(
		ctx,
		types.PortID,
		sourceChannel,
		clienttypes.ZeroHeight(),
		timeoutTimestamp,
		types.FungibleTokenPacketDataBytes(packetData),
	)
	if err != nil {
		return 0, err
	}

	events.EmitTransferEvent(ctx, packetData.Sender, packetData.Receiver, token, packetData.Memo)
	return sequence, nil
}
