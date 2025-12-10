package keeper

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	ibcerrors "github.com/cosmos/ibc-go/v10/modules/core/errors"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/internal/events"
	"github.com/gurufinglobal/guru/v2/x/ibc/transwap/types"
)

func (k Keeper) transferV1Packet(ctx sdk.Context, sourceChannel string, token types.Token, timeoutTimestamp uint64, packetData types.FungibleTokenPacketData) (uint64, error) { //nolint:unparam
	if err := k.SendTransfer(ctx, types.PortID, sourceChannel, token, sdk.MustAccAddressFromBech32(packetData.Sender)); err != nil {
		return 0, err
	}

	packetDataBytes := packetData.GetBytes()
	ibcHeight := clienttypes.ZeroHeight()
	sequence, err := k.ics4Wrapper.SendPacket(ctx, types.PortID, sourceChannel, ibcHeight, timeoutTimestamp, packetDataBytes)
	if err != nil {
		return 0, err
	}

	events.EmitTransferEvent(ctx, packetData.Sender, packetData.Receiver, token, packetData.Memo)

	return sequence, nil
}

func (k Keeper) transferV2Packet(ctx sdk.Context, encoding, sourceChannel string, timeoutTimestamp uint64, packetData types.FungibleTokenPacketData) (uint64, error) { //nolint:unparam
	if encoding == "" {
		encoding = types.EncodingJSON
	}

	data, err := types.MarshalPacketData(packetData, types.V1, encoding)
	if err != nil {
		return 0, err
	}

	payload := channeltypesv2.NewPayload(
		types.PortID, types.PortID,
		types.V1, encoding, data,
	)
	msg := channeltypesv2.NewMsgSendPacket(
		sourceChannel, timeoutTimestamp,
		packetData.Sender, payload,
	)

	handler := k.msgRouter.Handler(msg)
	if handler == nil {
		return 0, errorsmod.Wrapf(ibcerrors.ErrInvalidRequest, "unrecognized packet type: %T", msg)
	}
	res, err := handler(ctx, msg)
	if err != nil {
		return 0, err
	}

	// NOTE: The sdk msg handler creates a new EventManager, so events must be correctly propagated back to the current context
	ctx.EventManager().EmitEvents(res.GetEvents())

	// Each individual sdk.Result has exactly one Msg response. We aggregate here.
	msgResponse := res.MsgResponses[0]
	if msgResponse == nil {
		return 0, errorsmod.Wrapf(ibcerrors.ErrLogic, "got nil Msg response for msg %s", sdk.MsgTypeURL(msg))
	}
	var sendResponse channeltypesv2.MsgSendPacketResponse
	err = proto.Unmarshal(msgResponse.Value, &sendResponse)
	if err != nil {
		return 0, err
	}

	return sendResponse.Sequence, nil
}
