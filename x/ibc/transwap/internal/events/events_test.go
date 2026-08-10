package events

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestEmitTransferEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	token := types.Token{
		Denom:  types.NewDenom("uatom"),
		Amount: "10",
	}
	EmitTransferEvent(ctx, "sender", "receiver", &token, "memo")
	require.Len(t, ctx.EventManager().Events(), 2)
}

func TestEmitOnRecvPacketEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	data := types.NewInternalTransferRepresentation("0", &types.Token{Denom: types.NewDenom("uatom"), Amount: "10"}, "sender", "receiver", "memo")

	EmitOnRecvPacketEvent(ctx, &data, nil, nil)
	require.Len(t, ctx.EventManager().Events(), 2)

	errCtx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	ackErr := errors.New("boom")
	errAck := channeltypes.NewErrorAcknowledgement(ackErr)
	EmitOnRecvPacketEvent(errCtx, &data, errAck, ackErr)
	require.Len(t, errCtx.EventManager().Events(), 2)
}

func TestEmitOnAcknowledgementPacketEvent(t *testing.T) {
	ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	data := types.NewInternalTransferRepresentation("0", &types.Token{Denom: types.NewDenom("uatom"), Amount: "10"}, "sender", "receiver", "memo")

	resultAck := channeltypes.NewResultAcknowledgement([]byte{1})
	EmitOnAcknowledgementPacketEvent(ctx, &data, resultAck)
	require.Len(t, ctx.EventManager().Events(), 3)

	errCtx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
	errorAck := channeltypes.NewErrorAcknowledgement(errors.New("nope"))
	EmitOnAcknowledgementPacketEvent(errCtx, &data, errorAck)
	require.Len(t, errCtx.EventManager().Events(), 3)
}

func TestEmitOnTimeoutEventAndDenomEventPreserveLegacyPulsarJSON(t *testing.T) {
	tests := []struct {
		name      string
		denom     types.Denom
		tokenJSON string
		denomJSON string
	}{
		{
			name:      "native denom omits trace",
			denom:     types.NewDenom("uatom"),
			tokenJSON: `{"denom":{"base":"uatom"},"amount":"10"}`,
			denomJSON: `{"base":"uatom"}`,
		},
		{
			name:      "traced denom preserves public field names",
			denom:     types.NewDenom("uatom", types.NewHop(types.PortID, "channel-7")),
			tokenJSON: `{"denom":{"base":"uatom","trace":[{"port_id":"transwap","channel_id":"channel-7"}]},"amount":"10"}`,
			denomJSON: `{"base":"uatom","trace":[{"port_id":"transwap","channel_id":"channel-7"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithEventManager(sdk.NewEventManager())
			data := types.NewInternalTransferRepresentation(
				"0",
				&types.Token{Denom: tt.denom, Amount: "10"},
				"sender",
				"receiver",
				"memo",
			)

			EmitOnTimeoutEvent(ctx, &data)
			EmitDenomEvent(ctx, data.Token)
			events := ctx.EventManager().Events()
			require.Len(t, events, 3)
			require.Equal(t, types.AttributeKeyRefundTokens, events[0].Attributes[1].Key)
			require.Equal(t, tt.tokenJSON, events[0].Attributes[1].Value)
			require.Equal(t, types.AttributeKeyDenom, events[2].Attributes[1].Key)
			require.Equal(t, tt.denomJSON, events[2].Attributes[1].Value)
		})
	}
}

func TestMustMarshalJSONPanicsOnInvalidType(t *testing.T) {
	type nonMarshalable struct {
		F func()
	}

	require.Panics(t, func() {
		mustMarshalJSON(nonMarshalable{})
	})
}
