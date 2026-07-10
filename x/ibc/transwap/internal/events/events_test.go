package events

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"

	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestEmitTransferEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	token := transwapv1.Token{
		Denom:  types.NewDenom("uatom"),
		Amount: "10",
	}
	EmitTransferEvent(ctx, "sender", "receiver", token, "memo")
	require.Len(t, ctx.EventManager().Events(), 2)
}

func TestEmitOnRecvPacketEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	data := types.NewInternalTransferRepresentation("0", transwapv1.Token{Denom: types.NewDenom("uatom"), Amount: "10"}, "sender", "receiver", "memo")

	EmitOnRecvPacketEvent(ctx, data, nil, nil)
	require.Len(t, ctx.EventManager().Events(), 2)

	errCtx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	ackErr := errors.New("boom")
	errAck := channeltypes.NewErrorAcknowledgement(ackErr)
	EmitOnRecvPacketEvent(errCtx, data, errAck, ackErr)
	require.Len(t, errCtx.EventManager().Events(), 2)
}

func TestEmitOnAcknowledgementPacketEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	data := types.NewInternalTransferRepresentation("0", transwapv1.Token{Denom: types.NewDenom("uatom"), Amount: "10"}, "sender", "receiver", "memo")

	resultAck := channeltypes.NewResultAcknowledgement([]byte{1})
	EmitOnAcknowledgementPacketEvent(ctx, data, resultAck)
	require.Len(t, ctx.EventManager().Events(), 3)

	errCtx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	errorAck := channeltypes.NewErrorAcknowledgement(errors.New("nope"))
	EmitOnAcknowledgementPacketEvent(errCtx, data, errorAck)
	require.Len(t, errCtx.EventManager().Events(), 3)
}

func TestEmitOnTimeoutEventAndDenomEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	data := types.NewInternalTransferRepresentation("0", transwapv1.Token{Denom: types.NewDenom("uatom"), Amount: "10"}, "sender", "receiver", "memo")

	EmitOnTimeoutEvent(ctx, data)
	EmitDenomEvent(ctx, data.Token)
	require.Len(t, ctx.EventManager().Events(), 3)
}

func TestMustMarshalJSONPanicsOnInvalidType(t *testing.T) {
	type nonMarshalable struct {
		F func()
	}

	require.Panics(t, func() {
		mustMarshalJSON(nonMarshalable{})
	})
}
