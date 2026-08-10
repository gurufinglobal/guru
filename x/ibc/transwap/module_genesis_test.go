package transwap

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

func TestAppModuleGenesisRoundTrip(t *testing.T) {
	k, ctx, bank, bex, ics4 := setupIBCModuleAckRefund(t)
	am := NewAppModule(k)

	defaults := newTranswapGenesisFields()
	require.NoError(t, am.DefaultGenesis(defaults.target))
	require.NoError(t, am.ValidateGenesis(defaults.source))
	defaultState, err := readGenesisState(defaults.source, types.DefaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, types.PortID, defaultState.GetPortId())
	require.Empty(t, defaultState.GetDenoms())
	require.Empty(t, defaultState.GetTotalEscrowed())

	state := types.NewGenesisState(
		types.PortID,
		types.Denoms{
			types.NewDenom("uatom"),
			types.NewDenom("uusdc", types.NewHop(types.PortID, "channel-7")),
		},
		sdk.NewCoins(sdk.NewInt64Coin("uatom", 125)),
	)
	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	pending := refundRecordForGenesisTest(receiver, 12, types.RefundStatus_REFUND_STATUS_PENDING)
	inFlight := refundRecordForGenesisTest(receiver, 13, types.RefundStatus_REFUND_STATUS_IN_FLIGHT)
	retryable := refundRecordForGenesisTest(receiver, 14, types.RefundStatus_REFUND_STATUS_RETRYABLE)
	inFlight.ActivePacketSequence = 88
	inFlight.ActiveTimeoutTimestamp = 1_700_000_300_000_000_000
	inFlight.RetryCount = 1
	retryable.RetryCount = 1
	retryable.NextRetryHeight = 42

	pendingOutput := channeltypes.NewPacket(
		types.FungibleTokenPacketDataBytes(types.NewFungibleTokenPacketData(
			"uusdc",
			"100",
			bex.reserve.String(),
			receiver.String(),
			"genesis output",
		)),
		pending.GetOriginalOutputSequence(),
		pending.GetOriginalOutputPort(),
		pending.GetOriginalOutputChannel(),
		"xswap",
		"channel-1",
		clienttypes.ZeroHeight(),
		pending.GetOriginalTimeoutTimestamp(),
	)
	pending.OriginalOutputPacketCommitment = channeltypes.CommitPacket(pendingOutput)
	ics4.recordPacketCommitment(pendingOutput)

	activePacket := channeltypes.NewPacket(
		types.FungibleTokenPacketDataBytes(types.NewFungibleTokenPacketData(
			types.DenomPath(inFlight.Token.Denom),
			inFlight.Token.Amount,
			bex.reserve.String(),
			inFlight.GetReceiver(),
			inFlight.GetMemo(),
		)),
		inFlight.GetActivePacketSequence(),
		inFlight.GetRefundSourcePort(),
		inFlight.GetRefundSourceChannel(),
		"xswap",
		"channel-1",
		clienttypes.ZeroHeight(),
		inFlight.GetActiveTimeoutTimestamp(),
	)
	ics4.recordPacketCommitment(activePacket)
	state.Refunds = []*types.RefundRecord{pending, inFlight, retryable}

	require.NoError(t, bex.AddPendingLiability(ctx, 7, sdk.NewInt64Coin("uatom", 300)))
	require.NoError(t, bex.LockExchangeFee(ctx, 7, sdk.NewInt64Coin("uatom", 1)))
	bank.SetBalance(bex.reserve, sdk.NewCoins(sdk.NewInt64Coin("uatom", 199)))
	bank.SetBalance(
		types.GetEscrowAddress(types.PortID, "channel-0"),
		sdk.NewCoins(sdk.NewInt64Coin("uatom", 100)),
	)
	input := newTranswapGenesisFields()
	require.NoError(t, writeGenesisState(input.target, state))
	require.NoError(t, am.ValidateGenesis(input.source))
	require.NoError(t, am.InitGenesis(ctx, input.source))

	exported := newTranswapGenesisFields()
	require.NoError(t, am.ExportGenesis(ctx, exported.target))
	got, err := readGenesisState(exported.source, types.DefaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, state.GetPortId(), got.GetPortId())
	require.Equal(t, state.GetDenoms(), got.GetDenoms())
	require.Equal(t, state.GetRefunds(), got.GetRefunds())

	gotEscrowed, err := types.ProtoCoinsToSDK(got.GetTotalEscrowed())
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uatom", 125)), gotEscrowed)
}

func TestWriteGenesisStatePreservesLegacyPulsarEncodingJSON(t *testing.T) {
	state := &types.GenesisState{
		PortId: types.PortID,
		Denoms: types.Denoms{
			types.NewDenom("uatom"),
			types.NewDenom("uusdc", types.NewHop(types.PortID, "channel-7")),
		},
		TotalEscrowed: sdk.NewCoins(sdk.NewInt64Coin("uatom", 125)),
		Params: &types.Params{
			MaxRefundRetries:     3,
			RefundTimeoutWindow:  300_000_000_000,
			MinRelaySafetyMargin: 30_000_000_000,
		},
		Refunds: []*types.RefundRecord{{
			Id:                             "transwap/channel-7/12",
			Status:                         types.RefundStatus_REFUND_STATUS_PENDING,
			RefundSourcePort:               types.PortID,
			RefundSourceChannel:            "channel-0",
			Token:                          types.Token{Denom: types.NewDenom("uatom"), Amount: "100"},
			Receiver:                       "receiver",
			ClaimAddress:                   "claim-address",
			Memo:                           "memo",
			ExchangeId:                     "7",
			OriginalFee:                    sdk.NewInt64Coin("uatom", 1),
			OriginalTimeoutTimestamp:       9,
			OriginalTimeoutHeight:          &types.RefundHeight{RevisionNumber: 2, RevisionHeight: 99},
			OriginalOutputPort:             types.PortID,
			OriginalOutputChannel:          "channel-7",
			OriginalOutputSequence:         12,
			OriginalOutputPacketCommitment: []byte{1, 2, 3},
			VolumeReservation: &bextypes.VolumeReservation{
				ExchangeId:             7,
				Direction:              bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
				EpochStartUnix:         11,
				EpochSeconds:           60,
				Amount:                 "100",
				VolumeWindowGeneration: 2,
			},
		}},
	}

	fields := newTranswapGenesisFields()
	require.NoError(t, writeGenesisState(fields.target, state))
	require.Equal(t, `"transwap"`+"\n", string(fields.fields["port_id"]))
	require.Equal(
		t,
		`[{"base":"uatom"},{"base":"uusdc","trace":[{"port_id":"transwap","channel_id":"channel-7"}]}]`+"\n",
		string(fields.fields["denoms"]),
	)
	require.Equal(t, `[{"denom":"uatom","amount":"125"}]`+"\n", string(fields.fields["total_escrowed"]))
	require.Equal(
		t,
		`{"max_refund_retries":3,"refund_timeout_window":300000000000,"min_relay_safety_margin":30000000000}`+"\n",
		string(fields.fields["params"]),
	)
	require.Equal(
		t,
		`[{"id":"transwap/channel-7/12","status":1,"refund_source_port":"transwap","refund_source_channel":"channel-0","token":{"denom":{"base":"uatom"},"amount":"100"},"receiver":"receiver","claim_address":"claim-address","memo":"memo","exchange_id":"7","original_fee":{"denom":"uatom","amount":"1"},"original_timeout_timestamp":9,"original_timeout_height":{"revision_number":2,"revision_height":99},"original_output_port":"transwap","original_output_channel":"channel-7","original_output_sequence":12,"original_output_packet_commitment":"AQID","volume_reservation":{"exchange_id":7,"direction":1,"epoch_start_unix":11,"epoch_seconds":60,"amount":"100","volume_window_generation":2}}]`+"\n",
		string(fields.fields["refunds"]),
	)
}

func refundRecordForGenesisTest(
	receiver sdk.AccAddress,
	sequence uint64,
	status types.RefundStatus,
) *types.RefundRecord {
	feeAmount := int64(0)
	if status == types.RefundStatus_REFUND_STATUS_PENDING {
		feeAmount = 1
	}
	record := &types.RefundRecord{
		Id:                       types.RefundID(types.PortID, "channel-7", sequence),
		Status:                   status,
		RefundSourcePort:         types.PortID,
		RefundSourceChannel:      "channel-0",
		Token:                    types.Token{Denom: types.NewDenom("uatom"), Amount: "100"},
		Receiver:                 receiver.String(),
		ClaimAddress:             receiver.String(),
		Memo:                     "genesis refund",
		ExchangeId:               "7",
		OriginalFee:              types.SDKCoinToProto(sdk.NewInt64Coin("uatom", feeAmount)),
		OriginalTimeoutTimestamp: 1_700_000_000_000_000_000,
		OriginalTimeoutHeight:    &types.RefundHeight{RevisionNumber: 2, RevisionHeight: 99},
		OriginalOutputPort:       types.PortID,
		OriginalOutputChannel:    "channel-7",
		OriginalOutputSequence:   sequence,
		VolumeReservation:        genesisTestVolumeReservation(),
	}
	commitment := sha256.Sum256([]byte(record.GetId()))
	record.OriginalOutputPacketCommitment = commitment[:]
	return record
}

func TestReadGenesisStateAcceptsCanonicalProtoJSONUint64Strings(t *testing.T) {
	fields := newTranswapGenesisFields()
	fields.fields["params"] = []byte(`{
		"maxRefundRetries": 3,
		"refundTimeoutWindow": "300000000000",
		"minRelaySafetyMargin": "30000000000"
	}`)

	receiver := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	refundID := types.RefundID(types.PortID, "channel-7", 12)
	refund := &types.RefundRecord{
		Id:                       refundID,
		Status:                   types.RefundStatus_REFUND_STATUS_PENDING,
		RefundSourcePort:         types.PortID,
		RefundSourceChannel:      "channel-0",
		Token:                    types.Token{Denom: types.NewDenom("uatom"), Amount: "2"},
		Receiver:                 receiver.String(),
		ClaimAddress:             receiver.String(),
		ExchangeId:               "7",
		OriginalFee:              types.SDKCoinToProto(sdk.NewInt64Coin("uatom", 0)),
		OriginalTimeoutTimestamp: 1_700_000_000_000_000_000,
		OriginalTimeoutHeight:    &types.RefundHeight{RevisionNumber: 2, RevisionHeight: 99},
		OriginalOutputPort:       types.PortID,
		OriginalOutputChannel:    "channel-7",
		OriginalOutputSequence:   12,
		VolumeReservation:        genesisTestVolumeReservation(),
	}
	encoded, err := types.ModuleCdc.MarshalJSON(&types.GenesisState{Refunds: []*types.RefundRecord{refund}})
	require.NoError(t, err)
	var encodedFields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &encodedFields))
	fields.fields["refunds"] = encodedFields["refunds"]

	state, err := readGenesisState(fields.source, types.DefaultGenesisState())
	require.NoError(t, err)
	require.Equal(t, uint64(300_000_000_000), state.GetParams().GetRefundTimeoutWindow())
	require.Equal(t, uint64(30_000_000_000), state.GetParams().GetMinRelaySafetyMargin())
	require.Len(t, state.GetRefunds(), 1)
	require.Equal(t, refund.GetOriginalTimeoutTimestamp(), state.GetRefunds()[0].GetOriginalTimeoutTimestamp())
	require.Equal(t, refund.GetOriginalOutputSequence(), state.GetRefunds()[0].GetOriginalOutputSequence())
}

func genesisTestVolumeReservation() *bextypes.VolumeReservation {
	return &bextypes.VolumeReservation{
		ExchangeId:             7,
		Direction:              bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
		EpochSeconds:           bextypes.MinVolumeEpochSeconds,
		Amount:                 "100",
		VolumeWindowGeneration: 1,
	}
}

func TestAppModuleGenesisValidationRejectsInvalidStateBeforeWrite(t *testing.T) {
	tests := []struct {
		name  string
		state *types.GenesisState
	}{
		{
			name:  "invalid port",
			state: types.NewGenesisState(" ", nil, nil),
		},
		{
			name: "duplicate denom",
			state: types.NewGenesisState(types.PortID, types.Denoms{
				types.NewDenom("uatom"),
				types.NewDenom("uatom"),
			}, nil),
		},
		{
			name: "invalid total escrow denom",
			state: &types.GenesisState{
				PortId: types.PortID,
				TotalEscrowed: sdk.Coins{{
					Denom:  "bad denom",
					Amount: sdkmath.OneInt(),
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := newTranswapGenesisFields()
			require.NoError(t, writeGenesisState(fields.target, tt.state))
			require.Error(t, (AppModule{}).ValidateGenesis(fields.source))
		})
	}

	k, ctx, _, _, _ := setupIBCModuleAckRefund(t)
	am := NewAppModule(k)
	invalid := newTranswapGenesisFields()
	require.NoError(t, writeGenesisState(invalid.target, tests[1].state))
	require.Error(t, am.InitGenesis(ctx, invalid.source))
	require.Empty(t, k.GetPort(ctx))
	require.Empty(t, k.GetAllDenoms(ctx))
}

func TestAppModuleGenesisIOErrorsAreClassified(t *testing.T) {
	am := AppModule{}
	readErr := errors.New("read failed")
	err := am.ValidateGenesis(func(string) (io.ReadCloser, error) {
		return nil, readErr
	})
	require.ErrorIs(t, err, types.ErrReadGenesisField)

	malformed := newTranswapGenesisFields()
	malformed.fields["port_id"] = []byte(`{"unterminated"`)
	err = am.ValidateGenesis(malformed.source)
	require.ErrorIs(t, err, types.ErrDecodeGenesisField)

	err = am.DefaultGenesis(func(string) (io.WriteCloser, error) {
		return nil, nil
	})
	require.ErrorIs(t, err, types.ErrNilGenesisTargetWriter)
}

type transwapGenesisFields struct {
	fields map[string][]byte
}

func newTranswapGenesisFields() *transwapGenesisFields {
	return &transwapGenesisFields{fields: make(map[string][]byte)}
}

func (m *transwapGenesisFields) source(field string) (io.ReadCloser, error) {
	value, ok := m.fields[field]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (m *transwapGenesisFields) target(field string) (io.WriteCloser, error) {
	return &transwapGenesisFieldWriter{field: field, fields: m.fields}, nil
}

type transwapGenesisFieldWriter struct {
	bytes.Buffer
	field  string
	fields map[string][]byte
}

func (w *transwapGenesisFieldWriter) Close() error {
	w.fields[w.field] = append([]byte(nil), w.Bytes()...)
	return nil
}
