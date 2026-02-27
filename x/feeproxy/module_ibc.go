package feeproxy

import (
	"errors"
	"fmt"

	sdkmath "cosmossdk.io/math"

	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	"github.com/cosmos/ibc-go/v10/modules/core/exported"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/gurufinglobal/guru/v2/ibc"
	"github.com/gurufinglobal/guru/v2/x/feeproxy/keeper"
	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

var _ porttypes.IBCModule = &IBCMiddleware{}

// IBCMiddleware implements the ICS26 callbacks for the transfer middleware given
// the feeproxy keeper and the underlying application.
//
// This middleware is responsible for async settlement/refund of the fee locked in
// the feeproxy escrow module account.
type IBCMiddleware struct {
	*ibc.Module
	keeper keeper.Keeper
}

func NewIBCMiddleware(k keeper.Keeper, app porttypes.IBCModule) IBCMiddleware {
	if app == nil {
		panic(errors.New("underlying application cannot be nil"))
	}
	return IBCMiddleware{
		Module: ibc.NewModule(app),
		keeper: k,
	}
}

func (im IBCMiddleware) OnRecvPacket(
	ctx sdk.Context,
	channelVersion string,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) exported.Acknowledgement {
	ack := im.Module.OnRecvPacket(ctx, channelVersion, packet, relayer)
	// IMPORTANT: PFM may return nil ACK to defer acknowledgement writing.
	if ack == nil {
		return nil
	}
	return ack
}

func (im IBCMiddleware) OnAcknowledgementPacket(
	ctx sdk.Context,
	channelVersion string,
	packet channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	var ack channeltypes.Acknowledgement
	if err := transfertypes.ModuleCdc.UnmarshalJSON(acknowledgement, &ack); err != nil {
		return errors.Join(errortypes.ErrUnknownRequest, fmt.Errorf("cannot unmarshal ICS-20 transfer packet acknowledgement: %v", err))
	}

	lockedFee, found, err := im.keeper.GetLockedFee(ctx, packet.SourcePort, packet.SourceChannel, packet.Sequence)
	if err != nil {
		return err
	}
	if !found {
		return im.Module.OnAcknowledgementPacket(ctx, channelVersion, packet, acknowledgement, relayer)
	}

	if ack.Success() {
		// Settlement: feeproxy_escrow -> reserve_address (configured in x/feeproxy params)
		params, err := im.keeper.GetParams(sdk.WrapSDKContext(ctx))
		if err != nil {
			return err
		}
		reserveAddr, err := sdk.AccAddressFromBech32(params.ReserveAddress)
		if err != nil {
			return fmt.Errorf("invalid reserve_address %q: %w", params.ReserveAddress, err)
		}

		if err := im.keeper.BankKeeper().SendCoinsFromModuleToAccount(
			ctx,
			types.EscrowModuleName,
			reserveAddr,
			sdk.NewCoins(lockedFee),
		); err != nil {
			return err
		}

		im.keeper.DeleteLockedFee(ctx, packet.SourcePort, packet.SourceChannel, packet.Sequence)
		return im.Module.OnAcknowledgementPacket(ctx, channelVersion, packet, acknowledgement, relayer)
	}

	// Failure path: top-up transfer escrow and override packet amount to original (net + fee).
	updatedPacket, err := refundAndOverride(ctx, im.keeper, packet, lockedFee)
	if err != nil {
		return err
	}

	im.keeper.DeleteLockedFee(ctx, packet.SourcePort, packet.SourceChannel, packet.Sequence)
	return im.Module.OnAcknowledgementPacket(ctx, channelVersion, updatedPacket, acknowledgement, relayer)
}

func (im IBCMiddleware) OnTimeoutPacket(
	ctx sdk.Context,
	channelVersion string,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	lockedFee, found, err := im.keeper.GetLockedFee(ctx, packet.SourcePort, packet.SourceChannel, packet.Sequence)
	if err != nil {
		return err
	}
	if !found {
		return im.Module.OnTimeoutPacket(ctx, channelVersion, packet, relayer)
	}

	updatedPacket, err := refundAndOverride(ctx, im.keeper, packet, lockedFee)
	if err != nil {
		return err
	}

	im.keeper.DeleteLockedFee(ctx, packet.SourcePort, packet.SourceChannel, packet.Sequence)
	return im.Module.OnTimeoutPacket(ctx, channelVersion, updatedPacket, relayer)
}

func refundAndOverride(ctx sdk.Context, k keeper.Keeper, packet channeltypes.Packet, lockedFee sdk.Coin) (channeltypes.Packet, error) {
	// 1) Top-up the transfer escrow address so that the underlying transfer refund
	//    can unescrow the *original* amount (net + fee) without insufficient funds.
	escrowAddr := transfertypes.GetEscrowAddress(packet.SourcePort, packet.SourceChannel)
	if err := k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.EscrowModuleName, escrowAddr, sdk.NewCoins(lockedFee)); err != nil {
		return packet, err
	}

	// 2) Override packet data amount from Y to X=Y+fee, so PFM/transfer undo uses X.
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(packet.GetData(), &data); err != nil {
		return packet, errors.Join(errortypes.ErrUnknownRequest, fmt.Errorf("cannot unmarshal ICS-20 transfer packet data: %s", err.Error()))
	}

	amt, ok := sdkmath.NewIntFromString(data.Amount)
	if !ok {
		return packet, fmt.Errorf("invalid packet amount: %q", data.Amount)
	}
	amt = amt.Add(lockedFee.Amount)
	if !amt.IsPositive() {
		return packet, fmt.Errorf("invalid overridden amount: %s", amt.String())
	}
	data.Amount = amt.String()

	bz := transfertypes.ModuleCdc.MustMarshalJSON(&data)
	packet.Data = bz
	return packet, nil
}

