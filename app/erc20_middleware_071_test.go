package app

import (
	"errors"
	"testing"

	"cosmossdk.io/log/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/evm/x/erc20"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	"github.com/stretchr/testify/require"
)

func TestEVM071ERC20MiddlewareRejectsNonCanonicalAcknowledgement(t *testing.T) {
	packetData := transfertypes.NewFungibleTokenPacketData("agxn", "1", "sender", "receiver", "")
	packet := channeltypes.Packet{Data: packetData.GetBytes()}

	for _, tc := range []struct {
		name         string
		ack          channeltypes.Acknowledgement
		nonCanonical bool
		wantErr      error
		wantCalls    int
	}{
		{
			name:      "canonical result",
			ack:       channeltypes.NewResultAcknowledgement([]byte{1}),
			wantCalls: 1,
		},
		{
			name:      "canonical error",
			ack:       channeltypes.NewErrorAcknowledgement(errors.New("remote failure")),
			wantCalls: 1,
		},
		{
			name:         "non-canonical result",
			ack:          channeltypes.NewResultAcknowledgement([]byte{1}),
			nonCanonical: true,
			wantErr:      sdkerrors.ErrInvalidType,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underlying := &trackingIBCModule{}
			keeper := &trackingERC20IBCKeeper{}
			middleware := erc20.NewIBCMiddleware(keeper, underlying)

			acknowledgement := transfertypes.ModuleCdc.MustMarshalJSON(&tc.ack)
			if tc.nonCanonical {
				// JSON whitespace preserves the decoded acknowledgement but changes
				// the consensus-relevant byte representation.
				acknowledgement = append([]byte(" \n\t"), acknowledgement...)
			}

			err := middleware.OnAcknowledgementPacket(
				sdk.Context{},
				transfertypes.V1,
				packet,
				acknowledgement,
				nil,
			)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantCalls, underlying.acknowledgementCalls)
			require.Equal(t, tc.wantCalls, keeper.acknowledgementCalls)
		})
	}
}

// Embedding the full interfaces keeps these test doubles focused on the only
// callback that this dependency regression exercises. Any unexpected method
// call still panics through the nil embedded interface.
type trackingIBCModule struct {
	porttypes.IBCModule
	acknowledgementCalls int
}

func (m *trackingIBCModule) OnAcknowledgementPacket(
	sdk.Context,
	string,
	channeltypes.Packet,
	[]byte,
	sdk.AccAddress,
) error {
	m.acknowledgementCalls++
	return nil
}

type trackingERC20IBCKeeper struct {
	erc20types.Erc20Keeper
	acknowledgementCalls int
}

func (k *trackingERC20IBCKeeper) OnAcknowledgementPacket(
	sdk.Context,
	channeltypes.Packet,
	transfertypes.FungibleTokenPacketData,
	channeltypes.Acknowledgement,
) error {
	k.acknowledgementCalls++
	return nil
}

func (*trackingERC20IBCKeeper) Logger(sdk.Context) log.Logger {
	return log.NewNopLogger()
}
