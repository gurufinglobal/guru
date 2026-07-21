package types

import (
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
)

// MarshalLegacyPulsarJSON reproduces the encoding/json representation exposed
// by the historical public Pulsar structs. It is intentionally separate from
// protobuf JSON: module split-genesis fields and selected event attributes used
// encoding/json directly and therefore made the generated Go struct tags part
// of their external contract.
func MarshalLegacyPulsarJSON(value any) ([]byte, error) {
	switch value := value.(type) {
	case nil:
		return []byte("null"), nil
	case string:
		return json.Marshal(value)
	case Denom:
		return json.Marshal(legacyPulsarDenomJSONFrom(value))
	case *Denom:
		if value == nil {
			return []byte("null"), nil
		}
		return json.Marshal(legacyPulsarDenomJSONFrom(*value))
	case *Token:
		return json.Marshal(legacyPulsarTokenJSONFrom(value))
	case Denoms:
		return json.Marshal(legacyPulsarDenomsJSONFrom(value))
	case sdk.Coins:
		return json.Marshal(legacyPulsarCoinsJSONFrom(value))
	case *Params:
		return json.Marshal(legacyPulsarParamsJSONFrom(value))
	case *RefundRecord:
		return json.Marshal(legacyPulsarRefundRecordJSONFrom(value))
	case []*RefundRecord:
		return json.Marshal(legacyPulsarRefundRecordsJSONFrom(value))
	default:
		return nil, fmt.Errorf("unsupported TransSwap legacy Pulsar JSON type %T", value)
	}
}

type legacyPulsarHopJSON struct {
	PortID    string `json:"port_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

type legacyPulsarDenomJSON struct {
	Base  string                 `json:"base,omitempty"`
	Trace []*legacyPulsarHopJSON `json:"trace,omitempty"`
}

type legacyPulsarTokenJSON struct {
	Denom  *legacyPulsarDenomJSON `json:"denom,omitempty"`
	Amount string                 `json:"amount,omitempty"`
}

type legacyPulsarCoinJSON struct {
	Denom  string `json:"denom,omitempty"`
	Amount string `json:"amount,omitempty"`
}

type legacyPulsarParamsJSON struct {
	MaxRefundRetries     uint32 `json:"max_refund_retries,omitempty"`
	RefundTimeoutWindow  uint64 `json:"refund_timeout_window,omitempty"`
	MinRelaySafetyMargin uint64 `json:"min_relay_safety_margin,omitempty"`
}

type legacyPulsarRefundHeightJSON struct {
	RevisionNumber uint64 `json:"revision_number,omitempty"`
	RevisionHeight uint64 `json:"revision_height,omitempty"`
}

type legacyPulsarVolumeReservationJSON struct {
	ExchangeID             uint64                 `json:"exchange_id,omitempty"`
	Direction              bextypes.SwapDirection `json:"direction,omitempty"`
	EpochStartUnix         uint64                 `json:"epoch_start_unix,omitempty"`
	EpochSeconds           uint32                 `json:"epoch_seconds,omitempty"`
	Amount                 string                 `json:"amount,omitempty"`
	VolumeWindowGeneration uint64                 `json:"volume_window_generation,omitempty"`
}

type legacyPulsarRefundRecordJSON struct {
	ID                             string                             `json:"id,omitempty"`
	Status                         RefundStatus                       `json:"status,omitempty"`
	RefundSourcePort               string                             `json:"refund_source_port,omitempty"`
	RefundSourceChannel            string                             `json:"refund_source_channel,omitempty"`
	Token                          *legacyPulsarTokenJSON             `json:"token,omitempty"`
	Receiver                       string                             `json:"receiver,omitempty"`
	ClaimAddress                   string                             `json:"claim_address,omitempty"`
	Memo                           string                             `json:"memo,omitempty"`
	ExchangeID                     string                             `json:"exchange_id,omitempty"`
	OriginalFee                    *legacyPulsarCoinJSON              `json:"original_fee,omitempty"`
	OriginalTimeoutTimestamp       uint64                             `json:"original_timeout_timestamp,omitempty"`
	OriginalTimeoutHeight          *legacyPulsarRefundHeightJSON      `json:"original_timeout_height,omitempty"`
	OriginalOutputPort             string                             `json:"original_output_port,omitempty"`
	OriginalOutputChannel          string                             `json:"original_output_channel,omitempty"`
	OriginalOutputSequence         uint64                             `json:"original_output_sequence,omitempty"`
	ActivePacketSequence           uint64                             `json:"active_packet_sequence,omitempty"`
	ActiveTimeoutTimestamp         uint64                             `json:"active_timeout_timestamp,omitempty"`
	RetryCount                     uint32                             `json:"retry_count,omitempty"`
	OriginalOutputPacketCommitment []byte                             `json:"original_output_packet_commitment,omitempty"`
	NextRetryHeight                uint64                             `json:"next_retry_height,omitempty"`
	VolumeReservation              *legacyPulsarVolumeReservationJSON `json:"volume_reservation,omitempty"`
}

func legacyPulsarDenomJSONFrom(denom Denom) legacyPulsarDenomJSON {
	var trace []*legacyPulsarHopJSON
	if denom.Trace != nil {
		trace = make([]*legacyPulsarHopJSON, len(denom.Trace))
		for i := range denom.Trace {
			trace[i] = &legacyPulsarHopJSON{
				PortID:    denom.Trace[i].PortId,
				ChannelID: denom.Trace[i].ChannelId,
			}
		}
	}
	return legacyPulsarDenomJSON{Base: denom.Base, Trace: trace}
}

func legacyPulsarDenomPresent(denom Denom) bool {
	return denom.Base != "" || denom.Trace != nil
}

func legacyPulsarTokenJSONFrom(token *Token) *legacyPulsarTokenJSON {
	if token == nil {
		return nil
	}

	var denom *legacyPulsarDenomJSON
	if legacyPulsarDenomPresent(token.Denom) {
		converted := legacyPulsarDenomJSONFrom(token.Denom)
		denom = &converted
	}
	return &legacyPulsarTokenJSON{Denom: denom, Amount: token.Amount}
}

func legacyPulsarTokenPresent(token Token) bool {
	return token.Amount != "" || legacyPulsarDenomPresent(token.Denom)
}

func legacyPulsarDenomsJSONFrom(denoms Denoms) []*legacyPulsarDenomJSON {
	if denoms == nil {
		return nil
	}
	out := make([]*legacyPulsarDenomJSON, len(denoms))
	for i := range denoms {
		converted := legacyPulsarDenomJSONFrom(denoms[i])
		out[i] = &converted
	}
	return out
}

func legacyPulsarCoinJSONFrom(coin sdk.Coin) legacyPulsarCoinJSON {
	amount := ""
	if !coin.Amount.IsNil() {
		amount = coin.Amount.String()
	}
	return legacyPulsarCoinJSON{Denom: coin.Denom, Amount: amount}
}

func legacyPulsarCoinPresent(coin sdk.Coin) bool {
	return coin.Denom != "" || !coin.Amount.IsNil()
}

func legacyPulsarCoinsJSONFrom(coins sdk.Coins) []*legacyPulsarCoinJSON {
	if coins == nil {
		return nil
	}
	out := make([]*legacyPulsarCoinJSON, len(coins))
	for i := range coins {
		converted := legacyPulsarCoinJSONFrom(coins[i])
		out[i] = &converted
	}
	return out
}

func legacyPulsarParamsJSONFrom(params *Params) *legacyPulsarParamsJSON {
	if params == nil {
		return nil
	}
	return &legacyPulsarParamsJSON{
		MaxRefundRetries:     params.MaxRefundRetries,
		RefundTimeoutWindow:  params.RefundTimeoutWindow,
		MinRelaySafetyMargin: params.MinRelaySafetyMargin,
	}
}

func legacyPulsarRefundHeightJSONFrom(height *RefundHeight) *legacyPulsarRefundHeightJSON {
	if height == nil {
		return nil
	}
	return &legacyPulsarRefundHeightJSON{
		RevisionNumber: height.RevisionNumber,
		RevisionHeight: height.RevisionHeight,
	}
}

func legacyPulsarVolumeReservationJSONFrom(reservation *bextypes.VolumeReservation) *legacyPulsarVolumeReservationJSON {
	if reservation == nil {
		return nil
	}
	return &legacyPulsarVolumeReservationJSON{
		ExchangeID:             reservation.ExchangeId,
		Direction:              reservation.Direction,
		EpochStartUnix:         reservation.EpochStartUnix,
		EpochSeconds:           reservation.EpochSeconds,
		Amount:                 reservation.Amount,
		VolumeWindowGeneration: reservation.VolumeWindowGeneration,
	}
}

func legacyPulsarRefundRecordJSONFrom(record *RefundRecord) *legacyPulsarRefundRecordJSON {
	if record == nil {
		return nil
	}

	var token *legacyPulsarTokenJSON
	if legacyPulsarTokenPresent(record.Token) {
		token = legacyPulsarTokenJSONFrom(&record.Token)
	}
	var originalFee *legacyPulsarCoinJSON
	if legacyPulsarCoinPresent(record.OriginalFee) {
		converted := legacyPulsarCoinJSONFrom(record.OriginalFee)
		originalFee = &converted
	}

	return &legacyPulsarRefundRecordJSON{
		ID:                             record.Id,
		Status:                         record.Status,
		RefundSourcePort:               record.RefundSourcePort,
		RefundSourceChannel:            record.RefundSourceChannel,
		Token:                          token,
		Receiver:                       record.Receiver,
		ClaimAddress:                   record.ClaimAddress,
		Memo:                           record.Memo,
		ExchangeID:                     record.ExchangeId,
		OriginalFee:                    originalFee,
		OriginalTimeoutTimestamp:       record.OriginalTimeoutTimestamp,
		OriginalTimeoutHeight:          legacyPulsarRefundHeightJSONFrom(record.OriginalTimeoutHeight),
		OriginalOutputPort:             record.OriginalOutputPort,
		OriginalOutputChannel:          record.OriginalOutputChannel,
		OriginalOutputSequence:         record.OriginalOutputSequence,
		ActivePacketSequence:           record.ActivePacketSequence,
		ActiveTimeoutTimestamp:         record.ActiveTimeoutTimestamp,
		RetryCount:                     record.RetryCount,
		OriginalOutputPacketCommitment: record.OriginalOutputPacketCommitment,
		NextRetryHeight:                record.NextRetryHeight,
		VolumeReservation:              legacyPulsarVolumeReservationJSONFrom(record.VolumeReservation),
	}
}

func legacyPulsarRefundRecordsJSONFrom(records []*RefundRecord) []*legacyPulsarRefundRecordJSON {
	if records == nil {
		return nil
	}
	out := make([]*legacyPulsarRefundRecordJSON, len(records))
	for i := range records {
		out[i] = legacyPulsarRefundRecordJSONFrom(records[i])
	}
	return out
}
