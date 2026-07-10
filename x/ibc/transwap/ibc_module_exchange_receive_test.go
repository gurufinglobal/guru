package transwap

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	keeperpkg "github.com/gurufinglobal/guru/v3/x/ibc/transwap/keeper"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestIBCModuleOnRecvExchangePacketReturnsSuccessAckAndCommitsAccounting(t *testing.T) {
	k, ctx, bank, _, ics4 := setupIBCModuleAckRefund(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	reserve := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	sender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	inputDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	outputDenom := types.NewDenom("atgxkrw", types.NewHop(types.PortID, "channel-7"))
	inputIBCDenom := types.DenomIBCDenom(inputDenom)
	outputIBCDenom := types.DenomIBCDenom(outputDenom)

	recvBex := &moduleRecvExchangeBexKeeper{
		reserve:        reserve,
		outputDenom:    outputIBCDenom,
		amountOut:      "100",
		feeAmount:      "3",
		ledgers:        make(map[string]sdk.Coins),
		recordedVolume: sdkmath.ZeroInt(),
	}
	k.BexKeeper = recvBex
	im := NewIBCModule(k)
	k.SetDenom(ctx, outputDenom)
	bank.SetBalance(reserve, sdk.NewCoins(sdk.NewCoin(outputIBCDenom, sdkmath.NewInt(1000))))

	packetData := transwapv1.FungibleTokenPacketData{
		ExchangeId: "7",
		Denom:      "atgxusd",
		Amount:     "103",
		Sender:     sender.String(),
		Receiver:   receiver.String(),
		Memo:       "source memo",
	}
	packet := channeltypes.Packet{
		Sequence:           33,
		SourcePort:         "xswap",
		SourceChannel:      "channel-1",
		DestinationPort:    types.PortID,
		DestinationChannel: "channel-0",
		Data:               types.FungibleTokenPacketDataBytes(packetData),
		TimeoutTimestamp:   uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	}

	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	reserveBalances := bank.GetAllBalances(ctx, reserve)
	require.Equal(t, sdkmath.NewInt(900), reserveBalances.AmountOf(outputIBCDenom))
	require.Equal(t, sdkmath.NewInt(100), reserveBalances.AmountOf(inputIBCDenom))
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.Equal(t, sdkmath.NewInt(3), bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).AmountOf(inputIBCDenom))

	require.Len(t, ics4.sent, 1)
	require.Equal(t, types.PortID, ics4.sent[0].sourcePort)
	require.Equal(t, "channel-7", ics4.sent[0].sourceChannel)
	outboundData, err := types.UnmarshalPacketData(ics4.sent[0].data, types.V1, types.EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, "100", outboundData.Token.Amount)
	require.Equal(t, reserve.String(), outboundData.Sender)
	require.Equal(t, receiver.String(), outboundData.Receiver)
	require.Equal(t, "Station exchange", outboundData.Memo)
	require.Equal(t, types.DenomPath(outputDenom), types.DenomPath(outboundData.Token.Denom))

	refundKey := keeperpkg.GetRefundPacketDataKey(types.PortID, "channel-7", ics4.sent[0].sequence)
	refund, err := k.GetRefundPacketData(ctx, refundKey)
	require.NoError(t, err)
	require.Equal(t, reserve.String(), refund.Sender)
	require.Equal(t, sender.String(), refund.Receiver)
	require.Equal(t, "7", refund.ExchangeId)
	require.Equal(t, types.PortID, refund.SourcePort)
	require.Equal(t, "channel-0", refund.SourceChannel)
	require.Equal(t, inputIBCDenom, refund.GetFee().GetDenom())
	require.Equal(t, "3", refund.GetFee().GetAmount())

	require.Equal(t, sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))), recvBex.ledger("collected"))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(inputIBCDenom, sdkmath.NewInt(3))), recvBex.ledger("locked"))
	require.Equal(t, sdkmath.NewInt(100), recvBex.recordedVolume)
}

func TestIBCModuleOnRecvExchangePacketReturnsErrorAckForNonNumericExchangeID(t *testing.T) {
	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	im := NewIBCModule(k)

	reserve := bex.reserve
	reserveBalance := sdk.NewCoins(sdk.NewInt64Coin("atgxkrw", 123))
	bank.SetBalance(reserve, reserveBalance)

	packetData := transwapv1.FungibleTokenPacketData{
		ExchangeId: "not-a-number",
		Denom:      "atgxusd",
		Amount:     "103",
		Sender:     sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)).String(),
		Receiver:   sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20)).String(),
		Memo:       "source memo",
	}
	packet := channeltypes.Packet{
		Sequence:           35,
		SourcePort:         "xswap",
		SourceChannel:      "channel-1",
		DestinationPort:    types.PortID,
		DestinationChannel: "channel-0",
		Data:               types.FungibleTokenPacketDataBytes(packetData),
		TimeoutTimestamp:   uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	}

	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
	require.False(t, ack.Success())

	foundAckErr := false
	for _, event := range ctx.EventManager().Events() {
		for _, attr := range event.Attributes {
			if attr.Key != types.AttributeKeyAckError {
				continue
			}
			foundAckErr = true
			require.Contains(t, attr.Value, "unable to parse exchange id")
		}
	}
	require.True(t, foundAckErr)

	require.Equal(t, reserveBalance, bank.GetAllBalances(ctx, reserve))
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.Empty(t, bex.ledger("released"))
	require.Empty(t, bex.ledger("deducted"))
	require.Empty(t, ics4.sent)
	require.False(t, k.HasDenom(ctx, types.DenomHash(types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0")))))
}

func TestIBCModuleOnRecvPacketReturnsErrorAckForMalformedSemanticFields(t *testing.T) {
	overflowUint256 := "115792089237316195423570985008687907853269984665640564039457584007913129639936"

	tests := []struct {
		name            string
		mutate          func(*transwapv1.FungibleTokenPacketData)
		wantErrContains string
	}{
		{
			name: "blank denom",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Denom = " "
			},
			wantErrContains: "base denomination cannot be blank",
		},
		{
			name: "invalid hop",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Denom = "bad port/channel-0/ugxusd"
			},
			wantErrContains: "invalid hop source port ID",
		},
		{
			name: "zero amount",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Amount = "0"
			},
			wantErrContains: "amount must be strictly positive",
		},
		{
			name: "negative amount",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Amount = "-1"
			},
			wantErrContains: "amount must be strictly positive",
		},
		{
			name: "non numeric amount",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Amount = "nan"
			},
			wantErrContains: "unable to parse transfer amount",
		},
		{
			name: "uint256 overflow amount",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Amount = overflowUint256
			},
			wantErrContains: "unable to parse transfer amount",
		},
		{
			name: "blank sender",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Sender = " "
			},
			wantErrContains: "sender address cannot be blank",
		},
		{
			name: "blank receiver",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Receiver = " "
			},
			wantErrContains: "receiver address cannot be blank",
		},
		{
			name: "receiver too long",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Receiver = strings.Repeat("r", types.MaximumReceiverLength+1)
			},
			wantErrContains: "receiver address must not exceed",
		},
		{
			name: "memo too long",
			mutate: func(data *transwapv1.FungibleTokenPacketData) {
				data.Memo = strings.Repeat("m", types.MaximumMemoLength+1)
			},
			wantErrContains: "memo must not exceed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
			ctx = ctx.WithEventManager(sdk.NewEventManager())
			im := NewIBCModule(k)

			reserve := bex.reserve
			reserveBalance := sdk.NewCoins(sdk.NewInt64Coin("atgxkrw", 123))
			bank.SetBalance(reserve, reserveBalance)

			packetData := transwapv1.FungibleTokenPacketData{
				ExchangeId: "7",
				Denom:      "atgxusd",
				Amount:     "103",
				Sender:     sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)).String(),
				Receiver:   sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20)).String(),
				Memo:       "source memo",
			}
			tt.mutate(&packetData)

			packet := channeltypes.Packet{
				Sequence:           36,
				SourcePort:         "xswap",
				SourceChannel:      "channel-1",
				DestinationPort:    types.PortID,
				DestinationChannel: "channel-0",
				Data:               types.FungibleTokenPacketDataBytes(packetData),
				TimeoutTimestamp:   uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
			}

			ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
			require.False(t, ack.Success())

			foundAckErr := false
			for _, event := range ctx.EventManager().Events() {
				for _, attr := range event.Attributes {
					if attr.Key != types.AttributeKeyAckError {
						continue
					}
					foundAckErr = true
					require.Contains(t, attr.Value, tt.wantErrContains)
				}
			}
			require.True(t, foundAckErr)

			require.Equal(t, reserveBalance, bank.GetAllBalances(ctx, reserve))
			require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
			require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
			require.Empty(t, bex.ledger("released"))
			require.Empty(t, bex.ledger("deducted"))
			require.Empty(t, ics4.sent)
			require.False(t, k.HasDenom(ctx, types.DenomHash(types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0")))))
		})
	}
}

func TestIBCModuleOnRecvExchangePacketRejectsExpiredInheritedTimeoutWithoutMutation(t *testing.T) {
	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	im := NewIBCModule(k)

	reserve := bex.reserve
	reserveBalance := sdk.NewCoins(sdk.NewInt64Coin("atgxkrw", 123))
	bank.SetBalance(reserve, reserveBalance)

	packetData := transwapv1.FungibleTokenPacketData{
		ExchangeId: "7",
		Denom:      "atgxusd",
		Amount:     "103",
		Sender:     sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)).String(),
		Receiver:   sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20)).String(),
		Memo:       "source memo",
	}
	packet := channeltypes.Packet{
		Sequence:           37,
		SourcePort:         "xswap",
		SourceChannel:      "channel-1",
		DestinationPort:    types.PortID,
		DestinationChannel: "channel-0",
		Data:               types.FungibleTokenPacketDataBytes(packetData),
		TimeoutTimestamp:   uint64(ctx.BlockTime().Add(-time.Nanosecond).UnixNano()), //nolint:gosec // fixed test time is positive.
	}

	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
	require.False(t, ack.Success())

	foundAckErr := false
	for _, event := range ctx.EventManager().Events() {
		for _, attr := range event.Attributes {
			if attr.Key != types.AttributeKeyAckError {
				continue
			}
			foundAckErr = true
			require.Contains(t, attr.Value, "rejecting exchange packet due to insufficient inherited timeout")
			require.Contains(t, attr.Value, "inherited timeout timestamp is too close")
		}
	}
	require.True(t, foundAckErr)

	require.Equal(t, reserveBalance, bank.GetAllBalances(ctx, reserve))
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.Empty(t, bex.ledger("released"))
	require.Empty(t, bex.ledger("deducted"))
	require.Empty(t, ics4.sent)
	require.False(t, k.HasDenom(ctx, types.DenomHash(types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0")))))
}

func TestIBCModuleOnRecvExchangePacketReturnsErrorAckWhenRouteResolutionFails(t *testing.T) {
	k, ctx, bank, _, ics4 := setupIBCModuleAckRefund(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	reserve := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	recvBex := &moduleRecvExchangeBexKeeper{
		reserve:        reserve,
		resolveErr:     bextypes.ErrInvalidRoute.Wrap("route channel is closed"),
		ledgers:        make(map[string]sdk.Coins),
		recordedVolume: sdkmath.ZeroInt(),
	}
	k.BexKeeper = recvBex
	im := NewIBCModule(k)

	reserveBalance := sdk.NewCoins(sdk.NewInt64Coin("atgxkrw", 123))
	bank.SetBalance(reserve, reserveBalance)

	packetData := transwapv1.FungibleTokenPacketData{
		ExchangeId: "7",
		Denom:      "atgxusd",
		Amount:     "103",
		Sender:     sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)).String(),
		Receiver:   sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20)).String(),
		Memo:       "source memo",
	}
	packet := channeltypes.Packet{
		Sequence:           38,
		SourcePort:         "xswap",
		SourceChannel:      "channel-1",
		DestinationPort:    types.PortID,
		DestinationChannel: "channel-0",
		Data:               types.FungibleTokenPacketDataBytes(packetData),
		TimeoutTimestamp:   uint64(ctx.BlockTime().Add(time.Hour).UnixNano()), //nolint:gosec // fixed test time is positive.
	}

	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
	require.False(t, ack.Success())

	foundAckErr := false
	for _, event := range ctx.EventManager().Events() {
		for _, attr := range event.Attributes {
			if attr.Key != types.AttributeKeyAckError {
				continue
			}
			foundAckErr = true
			require.Contains(t, attr.Value, "failed to resolve swap direction")
			require.Contains(t, attr.Value, "route channel is closed")
		}
	}
	require.True(t, foundAckErr)

	require.Zero(t, recvBex.quoteCalls)
	require.Equal(t, sdkmath.ZeroInt(), recvBex.recordedVolume)
	require.Equal(t, reserveBalance, bank.GetAllBalances(ctx, reserve))
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(bextypes.ModuleName)).IsZero())
	require.Empty(t, recvBex.ledger("collected"))
	require.Empty(t, recvBex.ledger("locked"))
	require.Empty(t, ics4.sent)
	require.False(t, k.HasDenom(ctx, types.DenomHash(types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0")))))
}

func TestIBCModuleOnRecvTransferPacketReturnsSuccessAckAndMintsVoucher(t *testing.T) {
	k, ctx, bank, _, _ := setupIBCModuleAckRefund(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	im := NewIBCModule(k)

	sender := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	packetData := types.NewFungibleTokenPacketData(
		"atgxusd",
		"42",
		sender.String(),
		receiver.String(),
		"normal transfer",
	)
	packet := channeltypes.Packet{
		Sequence:           34,
		SourcePort:         "xswap",
		SourceChannel:      "channel-1",
		DestinationPort:    types.PortID,
		DestinationChannel: "channel-0",
		Data:               types.FungibleTokenPacketDataBytes(packetData),
	}

	ack := im.OnRecvPacket(ctx, types.V1, packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	voucherDenom := types.NewDenom("atgxusd", types.NewHop(types.PortID, "channel-0"))
	voucherIBCDenom := types.DenomIBCDenom(voucherDenom)
	require.True(t, k.HasDenom(ctx, types.DenomHash(voucherDenom)))
	require.Equal(t, sdkmath.NewInt(42), bank.GetAllBalances(ctx, receiver).AmountOf(voucherIBCDenom))
	require.True(t, bank.GetAllBalances(ctx, authtypes.NewModuleAddress(types.ModuleName)).IsZero())
}

type moduleRecvExchangeBexKeeper struct {
	reserve        sdk.AccAddress
	outputDenom    string
	amountOut      string
	feeAmount      string
	resolveErr     error
	quoteCalls     int
	ledgers        map[string]sdk.Coins
	recordedVolume sdkmath.Int
}

func (m *moduleRecvExchangeBexKeeper) ResolveSwapDirection(_ context.Context, exchangeID uint64, inputDenom string) (bexv1.SwapDirection, error) {
	if m.resolveErr != nil {
		return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, m.resolveErr
	}
	if exchangeID != 7 || inputDenom != "atgxusd" {
		return bexv1.SwapDirection_SWAP_DIRECTION_UNSPECIFIED, bextypes.ErrInvalidRoute.Wrap("unexpected exchange route")
	}
	return bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B, nil
}

func (m *moduleRecvExchangeBexKeeper) QuoteSwap(_ context.Context, req *bexv1.QuoteSwapRequest) (*bexv1.QuoteSwapResponse, error) {
	m.quoteCalls++
	if req.GetExchangeId() != 7 || req.GetInputDenom() != "atgxusd" || req.GetAmountIn() != "103" {
		return nil, bextypes.ErrInvalidRoute.Wrap("unexpected exchange quote")
	}
	return &bexv1.QuoteSwapResponse{
		ExchangeId:  req.GetExchangeId(),
		Direction:   bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		InputDenom:  req.GetInputDenom(),
		OutputDenom: m.outputDenom,
		AmountIn:    req.GetAmountIn(),
		FeeAmount:   m.feeAmount,
		NetAmountIn: "100",
		AmountOut:   m.amountOut,
	}, nil
}

func (*moduleRecvExchangeBexKeeper) WithReserveReceiveAllowance(ctx context.Context, _ uint64) context.Context {
	return ctx
}

func (m *moduleRecvExchangeBexKeeper) RecordVolumeWindow(_ context.Context, _ uint64, direction bexv1.SwapDirection, amountOut sdkmath.Int) error {
	if direction != bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B {
		return bextypes.ErrInvalidRoute.Wrap("unexpected volume direction")
	}
	m.recordedVolume = m.recordedVolume.Add(amountOut)
	return nil
}

func (m *moduleRecvExchangeBexKeeper) AddCollectedFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.addLedger("collected", fee)
	return nil
}

func (m *moduleRecvExchangeBexKeeper) LockExchangeFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.addLedger("locked", fee)
	return nil
}

func (m *moduleRecvExchangeBexKeeper) ReleaseExchangeFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.addLedger("released", fee)
	return nil
}

func (m *moduleRecvExchangeBexKeeper) DeductCollectedFee(_ context.Context, _ uint64, fee sdk.Coin) error {
	m.addLedger("deducted", fee)
	return nil
}

func (m *moduleRecvExchangeBexKeeper) GetReserveAddress(context.Context, uint64) sdk.AccAddress {
	return m.reserve
}

func (m *moduleRecvExchangeBexKeeper) addLedger(kind string, coin sdk.Coin) {
	m.ledgers[kind] = m.ledgers[kind].Add(coin)
}

func (m *moduleRecvExchangeBexKeeper) ledger(kind string) sdk.Coins {
	return m.ledgers[kind]
}
