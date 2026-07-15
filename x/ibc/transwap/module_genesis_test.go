package transwap

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
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
	pending := refundRecordForGenesisTest(receiver, 12, transwapv1.RefundStatus_REFUND_STATUS_PENDING)
	inFlight := refundRecordForGenesisTest(receiver, 13, transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT)
	retryable := refundRecordForGenesisTest(receiver, 14, transwapv1.RefundStatus_REFUND_STATUS_RETRYABLE)
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
			types.DenomPath(inFlight.GetToken().GetDenom()),
			inFlight.GetToken().GetAmount(),
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
	state.Refunds = []*transwapv1.RefundRecord{pending, inFlight, retryable}

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

func refundRecordForGenesisTest(
	receiver sdk.AccAddress,
	sequence uint64,
	status transwapv1.RefundStatus,
) *transwapv1.RefundRecord {
	feeAmount := int64(0)
	if status == transwapv1.RefundStatus_REFUND_STATUS_PENDING {
		feeAmount = 1
	}
	record := &transwapv1.RefundRecord{
		Id:                       types.RefundID(types.PortID, "channel-7", sequence),
		Status:                   status,
		RefundSourcePort:         types.PortID,
		RefundSourceChannel:      "channel-0",
		Token:                    &transwapv1.Token{Denom: types.NewDenom("uatom"), Amount: "100"},
		Receiver:                 receiver.String(),
		ClaimAddress:             receiver.String(),
		Memo:                     "genesis refund",
		ExchangeId:               "7",
		OriginalFee:              types.SDKCoinToProto(sdk.NewInt64Coin("uatom", feeAmount)),
		OriginalTimeoutTimestamp: 1_700_000_000_000_000_000,
		OriginalTimeoutHeight:    &transwapv1.RefundHeight{RevisionNumber: 2, RevisionHeight: 99},
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
	refund := &transwapv1.RefundRecord{
		Id:                       refundID,
		Status:                   transwapv1.RefundStatus_REFUND_STATUS_PENDING,
		RefundSourcePort:         types.PortID,
		RefundSourceChannel:      "channel-0",
		Token:                    &transwapv1.Token{Denom: types.NewDenom("uatom"), Amount: "2"},
		Receiver:                 receiver.String(),
		ClaimAddress:             receiver.String(),
		ExchangeId:               "7",
		OriginalFee:              types.SDKCoinToProto(sdk.NewInt64Coin("uatom", 0)),
		OriginalTimeoutTimestamp: 1_700_000_000_000_000_000,
		OriginalTimeoutHeight:    &transwapv1.RefundHeight{RevisionNumber: 2, RevisionHeight: 99},
		OriginalOutputPort:       types.PortID,
		OriginalOutputChannel:    "channel-7",
		OriginalOutputSequence:   12,
		VolumeReservation:        genesisTestVolumeReservation(),
	}
	encoded, err := protojson.Marshal(&transwapv1.GenesisState{Refunds: []*transwapv1.RefundRecord{refund}})
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

func genesisTestVolumeReservation() *bexv1.VolumeReservation {
	return &bexv1.VolumeReservation{
		ExchangeId:             7,
		Direction:              bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
		EpochSeconds:           bextypes.MinVolumeEpochSeconds,
		Amount:                 "100",
		VolumeWindowGeneration: 1,
	}
}

func TestAppModuleGenesisValidationRejectsInvalidStateBeforeWrite(t *testing.T) {
	tests := []struct {
		name  string
		state *transwapv1.GenesisState
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
			state: &transwapv1.GenesisState{
				PortId: types.PortID,
				TotalEscrowed: []*basev1beta1.Coin{{
					Denom:  "bad denom",
					Amount: "1",
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
