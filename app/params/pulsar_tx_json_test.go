package params

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

type pulsarQueryClient struct {
	client.MockClient
	txResult    *coretypes.ResultTx
	block       *coretypes.ResultBlock
	blockHeight int64
}

func (c pulsarQueryClient) Tx(_ context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error) {
	if !prove || !bytes.Equal(hash, c.txResult.Hash) {
		return nil, fmt.Errorf("unexpected tx query: hash=%X prove=%t", hash, prove)
	}
	return c.txResult, nil
}

func (c pulsarQueryClient) Block(_ context.Context, height *int64) (*coretypes.ResultBlock, error) {
	if height == nil || *height != c.blockHeight {
		return nil, fmt.Errorf("unexpected block query height: %v", height)
	}
	return c.block, nil
}

func TestPulsarDecodedTxSupportsJSONAndQueryTxRepresentation(t *testing.T) {
	configureSDKBech32ForTest()

	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)
	oracletypes.RegisterInterfaces(encoding.InterfaceRegistry)

	moderatorBytes := bytes.Repeat([]byte{0x11}, 20)
	moderator, err := encoding.InterfaceRegistry.SigningContext().AddressCodec().BytesToString(moderatorBytes)
	if err != nil {
		t.Fatalf("moderator address: %v", err)
	}

	builder := encoding.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(&oraclev1.MsgUpsertTask{
		Moderator: moderator,
		Task: &oraclev1.OracleTask{
			Symbol:             "BTC/USD",
			ValueType:          oraclev1.ValueType_VALUE_TYPE_NUMERIC,
			Enabled:            true,
			SubmissionInterval: 5,
		},
	}); err != nil {
		t.Fatalf("set Pulsar message: %v", err)
	}

	// Preserve the base TxConfig behavior for ordinary SDK transaction wrappers.
	if _, err := encoding.TxConfig.TxJSONEncoder()(builder.GetTx()); err != nil {
		t.Fatalf("JSON encode SDK transaction wrapper: %v", err)
	}

	txBytes, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}
	decoded, err := encoding.TxConfig.TxDecoder()(txBytes)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	if _, ok := decoded.(*pulsarDecodedTx); !ok {
		t.Fatalf("decoded tx type = %T, want *pulsarDecodedTx", decoded)
	}

	jsonTx, err := encoding.TxConfig.TxJSONEncoder()(decoded)
	if err != nil {
		t.Fatalf("JSON encode decoded Pulsar tx: %v", err)
	}
	if !json.Valid(jsonTx) {
		t.Fatalf("decoded Pulsar tx is not valid JSON: %s", jsonTx)
	}
	if !bytes.Contains(jsonTx, []byte(`"@type":"/guru.oracle.v1.MsgUpsertTask"`)) ||
		!bytes.Contains(jsonTx, []byte(`"symbol":"BTC/USD"`)) {
		t.Fatalf("decoded Pulsar JSON lost nested message data: %s", jsonTx)
	}

	// Exercise the SDK query-tx path: RPC Tx/Block lookup, application decoder,
	// AsAny conversion, and the same codec used by the CLI's PrintProto call.
	const height int64 = 7
	blockTime := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	hash := bytes.Repeat([]byte{0x42}, 32)
	node := pulsarQueryClient{
		txResult: &coretypes.ResultTx{
			Hash:   hash,
			Height: height,
			Tx:     cmttypes.Tx(txBytes),
		},
		block: &coretypes.ResultBlock{
			Block: &cmttypes.Block{Header: cmttypes.Header{Height: height, Time: blockTime}},
		},
		blockHeight: height,
	}

	var output bytes.Buffer
	clientCtx := client.Context{}.
		WithClient(node).
		WithTxConfig(encoding.TxConfig).
		WithCodec(encoding.Codec).
		WithInterfaceRegistry(encoding.InterfaceRegistry).
		WithOutput(&output).
		WithOutputFormat("json")
	response, err := authtx.QueryTx(clientCtx, hex.EncodeToString(hash))
	if err != nil {
		t.Fatalf("query tx: %v", err)
	}
	protoTx, ok := response.Tx.GetCachedValue().(*txtypes.Tx)
	if !ok {
		t.Fatalf("query response tx type = %T, want *tx.Tx", response.Tx.GetCachedValue())
	}
	if len(protoTx.GetBody().GetMessages()) != 1 || protoTx.GetBody().GetMessages()[0].GetTypeUrl() != "/guru.oracle.v1.MsgUpsertTask" {
		t.Fatalf("query response lost Pulsar message: %+v", protoTx.GetBody().GetMessages())
	}
	if err := clientCtx.PrintProto(response); err != nil {
		t.Fatalf("print query tx response: %v", err)
	}
	if !json.Valid(output.Bytes()) || !bytes.Contains(output.Bytes(), []byte(`"symbol":"BTC/USD"`)) {
		t.Fatalf("query tx JSON lost nested message data: %s", output.Bytes())
	}
}

func TestPulsarTxJSONEncoderRejectsNilDecodedTx(t *testing.T) {
	encoding := MakeEncodingConfig(Bech32PrefixAccAddr, Bech32PrefixValAddr, Bech32PrefixConsAddr)
	encoder := encoding.TxConfig.TxJSONEncoder()

	for _, tx := range []sdk.Tx{(*pulsarDecodedTx)(nil), &pulsarDecodedTx{}} {
		if _, err := encoder(tx); err == nil {
			t.Fatalf("JSON encode nil Pulsar tx %T: expected error", tx)
		}
	}
}
