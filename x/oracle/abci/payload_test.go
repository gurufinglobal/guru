package abci

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

func TestProposalTxUsesCanonicalSDKEnvelope(t *testing.T) {
	payload := proposalPayloadFixture()

	txBytes, err := EncodeProposalTx(payload)
	require.NoError(t, err)

	var raw txtypes.TxRaw
	require.NoError(t, raw.Unmarshal(txBytes))
	require.Empty(t, raw.Signatures)

	var body txtypes.TxBody
	require.NoError(t, body.Unmarshal(raw.BodyBytes))
	require.Empty(t, body.Messages)
	require.Empty(t, body.Memo)
	require.Zero(t, body.TimeoutHeight)
	require.False(t, body.Unordered)
	require.Nil(t, body.TimeoutTimestamp)
	require.Len(t, body.ExtensionOptions, 1)
	require.Equal(t, "/guru.oracle.v1.OracleProposalPayload", body.ExtensionOptions[0].TypeUrl)
	require.Empty(t, body.NonCriticalExtensionOptions)

	var authInfo txtypes.AuthInfo
	require.NoError(t, authInfo.Unmarshal(raw.AuthInfoBytes))
	require.Empty(t, authInfo.SignerInfos)
	require.NotNil(t, authInfo.Fee)
	require.Empty(t, authInfo.Fee.Amount)
	require.Zero(t, authInfo.Fee.GasLimit)
	require.Empty(t, authInfo.Fee.Payer)
	require.Empty(t, authInfo.Fee.Granter)
	require.Nil(t, authInfo.Tip)

	registry := codectypes.NewInterfaceRegistry()
	oracletypes.RegisterInterfaces(registry)
	decoder := authtx.DefaultTxDecoder(codec.NewProtoCodec(registry))
	decodedTx, err := decoder(txBytes)
	require.NoError(t, err)
	require.Empty(t, decodedTx.GetMsgs())

	decodedPayload, isProposal, err := DecodeProposalTx(txBytes)
	require.NoError(t, err)
	require.True(t, isProposal)
	require.Equal(t, payload, decodedPayload)

	reencoded, err := EncodeProposalTx(decodedPayload)
	require.NoError(t, err)
	require.Equal(t, txBytes, reencoded)
}

func TestProposalTxClassificationRejectsNonCanonicalCandidates(t *testing.T) {
	canonical, err := EncodeProposalTx(proposalPayloadFixture())
	require.NoError(t, err)

	tests := []struct {
		name          string
		tx            func(t *testing.T) []byte
		wantCandidate bool
		wantCanonical bool
	}{
		{
			name:          "canonical",
			tx:            func(*testing.T) []byte { return canonical },
			wantCandidate: true,
			wantCanonical: true,
		},
		{
			name: "ordinary invalid bytes",
			tx:   func(*testing.T) []byte { return []byte("ordinary") },
		},
		{
			name: "ordinary SDK envelope",
			tx: func(t *testing.T) []byte {
				return marshalProposalTxParts(t, &txtypes.TxRaw{}, &txtypes.TxBody{}, &txtypes.AuthInfo{})
			},
		},
		{
			name: "altered type URL is ordinary",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.ExtensionOptions[0].TypeUrl = "/guru.oracle.v1.NotOracleProposalPayload"
			}),
		},
		{
			name: "extra message",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.Messages = []*codectypes.Any{{TypeUrl: "/test.Msg"}}
			}),
			wantCandidate: true,
		},
		{
			name: "memo",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.Memo = "not canonical"
			}),
			wantCandidate: true,
		},
		{
			name: "timeout height",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.TimeoutHeight = 1
			}),
			wantCandidate: true,
		},
		{
			name: "unordered timeout",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				timestamp := time.Unix(100, 0).UTC()
				body.Unordered = true
				body.TimeoutTimestamp = &timestamp
			}),
			wantCandidate: true,
		},
		{
			name: "extra critical option",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.ExtensionOptions = append(body.ExtensionOptions, &codectypes.Any{TypeUrl: "/test.Option"})
			}),
			wantCandidate: true,
		},
		{
			name: "duplicate Oracle option",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.ExtensionOptions = append(body.ExtensionOptions, body.ExtensionOptions[0])
			}),
			wantCandidate: true,
		},
		{
			name: "Oracle option moved to non-critical list",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.NonCriticalExtensionOptions = body.ExtensionOptions
				body.ExtensionOptions = nil
			}),
			wantCandidate: true,
		},
		{
			name: "invalid Oracle payload bytes",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, body *txtypes.TxBody, _ *txtypes.AuthInfo) {
				body.ExtensionOptions[0].Value = []byte{0xff}
			}),
			wantCandidate: true,
		},
		{
			name: "signer info",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, _ *txtypes.TxBody, authInfo *txtypes.AuthInfo) {
				authInfo.SignerInfos = []*txtypes.SignerInfo{{}}
			}),
			wantCandidate: true,
		},
		{
			name: "non-zero fee",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, _ *txtypes.TxBody, authInfo *txtypes.AuthInfo) {
				authInfo.Fee.GasLimit = 1
			}),
			wantCandidate: true,
		},
		{
			name: "tip",
			tx: mutateProposalTx(t, canonical, func(_ *txtypes.TxRaw, _ *txtypes.TxBody, authInfo *txtypes.AuthInfo) {
				authInfo.Tip = &txtypes.Tip{} //nolint:staticcheck // candidate validation intentionally covers the deprecated SDK tip field.
			}),
			wantCandidate: true,
		},
		{
			name: "signature",
			tx: mutateProposalTx(t, canonical, func(raw *txtypes.TxRaw, _ *txtypes.TxBody, _ *txtypes.AuthInfo) {
				raw.Signatures = [][]byte{{0x01}}
			}),
			wantCandidate: true,
		},
		{
			name: "unknown root field",
			tx: func(*testing.T) []byte {
				return append(append([]byte(nil), canonical...), 0x22, 0x00)
			},
			wantCandidate: true,
		},
		{
			name: "non-canonical root field order",
			tx: func(t *testing.T) []byte {
				var raw txtypes.TxRaw
				require.NoError(t, raw.Unmarshal(canonical))
				reordered := appendLengthDelimitedField(nil, 2, raw.AuthInfoBytes)
				return appendLengthDelimitedField(reordered, 1, raw.BodyBytes)
			},
			wantCandidate: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			txBytes := tc.tx(t)
			payload, isCandidate, err := DecodeProposalTx(txBytes)
			require.Equal(t, tc.wantCandidate, isCandidate)
			if tc.wantCanonical {
				require.NoError(t, err)
				require.NotNil(t, payload)
			} else if tc.wantCandidate {
				require.Error(t, err)
				require.Nil(t, payload)
			} else {
				require.NoError(t, err)
				require.Nil(t, payload)
			}
			require.Equal(t, tc.wantCanonical, IsProposalTx(txBytes))
		})
	}
}

func TestEncodeProposalTxRejectsNilPayload(t *testing.T) {
	_, err := EncodeProposalTx(nil)
	require.ErrorContains(t, err, "cannot be nil")
}

func proposalPayloadFixture() *oracletypes.OracleProposalPayload {
	return &oracletypes.OracleProposalPayload{
		Height: 7,
		Values: []*oracletypes.OracleValue{{
			Symbol:      "BTC/USD",
			ValueType:   oracletypes.ValueType_VALUE_TYPE_NUMERIC,
			Value:       "65000.25",
			BlockHeight: 7,
		}},
	}
}

func mutateProposalTx(
	t *testing.T,
	canonical []byte,
	mutate func(raw *txtypes.TxRaw, body *txtypes.TxBody, authInfo *txtypes.AuthInfo),
) func(t *testing.T) []byte {
	t.Helper()

	return func(t *testing.T) []byte {
		var raw txtypes.TxRaw
		require.NoError(t, raw.Unmarshal(canonical))
		var body txtypes.TxBody
		require.NoError(t, body.Unmarshal(raw.BodyBytes))
		var authInfo txtypes.AuthInfo
		require.NoError(t, authInfo.Unmarshal(raw.AuthInfoBytes))

		mutate(&raw, &body, &authInfo)
		return marshalProposalTxParts(t, &raw, &body, &authInfo)
	}
}

func marshalProposalTxParts(t *testing.T, raw *txtypes.TxRaw, body *txtypes.TxBody, authInfo *txtypes.AuthInfo) []byte {
	t.Helper()

	var err error
	raw.BodyBytes, err = body.Marshal()
	require.NoError(t, err)
	raw.AuthInfoBytes, err = authInfo.Marshal()
	require.NoError(t, err)
	txBytes, err := raw.Marshal()
	require.NoError(t, err)
	return txBytes
}

func appendLengthDelimitedField(dst []byte, fieldNumber byte, value []byte) []byte {
	dst = append(dst, fieldNumber<<3|2)
	var length [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(length[:], uint64(len(value)))
	dst = append(dst, length[:n]...)
	return append(dst, value...)
}
