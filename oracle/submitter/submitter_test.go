package submitter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	guruconfig "github.com/gurufinglobal/guru/v2/cmd/gurud/config"
	"github.com/gurufinglobal/guru/v2/crypto/hd"
	guruencoding "github.com/gurufinglobal/guru/v2/encoding"
	cosmosevmtypes "github.com/gurufinglobal/guru/v2/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
	"google.golang.org/grpc"
)

type mockSubmitClient struct {
	mu     sync.Mutex
	calls  int
	resp   *sdk.TxResponse
	err    error
	lastTx []byte
}

func (m *mockSubmitClient) BroadcastTx(txBytes []byte) (*sdk.TxResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastTx = append([]byte(nil), txBytes...)
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

type countingAuthClient struct {
	mu    sync.Mutex
	calls int
	resps []*authtypes.QueryAccountInfoResponse
	err   error
}

func (c *countingAuthClient) AccountInfo(ctx context.Context, in *authtypes.QueryAccountInfoRequest, _ ...grpc.CallOption) (*authtypes.QueryAccountInfoResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	if len(c.resps) == 0 {
		return &authtypes.QueryAccountInfoResponse{Info: &authtypes.BaseAccount{}}, nil
	}
	if c.calls-1 >= len(c.resps) {
		return c.resps[len(c.resps)-1], nil
	}
	return c.resps[c.calls-1], nil
}

var bech32Once sync.Once

func setupBech32(t *testing.T) {
	t.Helper()
	bech32Once.Do(func() {
		cfg := sdk.GetConfig()
		cfg.SetBech32PrefixForAccount("guru", "gurupub")
		cfg.SetBech32PrefixForValidator("guruv", "guruvpub")
		cfg.SetBech32PrefixForConsensusNode("guruc", "gurucpub")
		cfg.Seal()
	})
}

func newTestKeyringAndAddress(t *testing.T, cdc codec.Codec, keyName string) (keyring.Keyring, sdk.AccAddress) {
	t.Helper()

	dir := t.TempDir()
	in := strings.NewReader("")
	kr, err := keyring.New("guru", keyring.BackendTest, dir, in, cdc, hd.EthSecp256k1Option())
	if err != nil {
		t.Fatalf("keyring.New error: %v", err)
	}
	in.Reset("password\npassword\n")
	rec, _, err := kr.NewMnemonic(keyName, keyring.English, cosmosevmtypes.BIP44HDPath, keyring.DefaultBIP39Passphrase, hd.EthSecp256k1)
	if err != nil {
		t.Fatalf("NewMnemonic error: %v", err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		t.Fatalf("GetAddress error: %v", err)
	}
	return kr, addr
}

func TestSubmitter_Submit_SuccessIncrementsSequence(t *testing.T) {
	t.Parallel()
	setupBech32(t)

	encCfg := guruencoding.MakeConfig(guruconfig.GuruChainID)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	keyName := "k1"
	kr, addr := newTestKeyringAndAddress(t, encCfg.Codec, keyName)

	auth := &countingAuthClient{}
	ai := NewAccountInfo(auth, addr)
	atomic.StoreUint64(&ai.accountNumber, 10)
	atomic.StoreUint64(&ai.sequenceNumber, 7)

	subClient := &mockSubmitClient{resp: &sdk.TxResponse{Code: 0, TxHash: "ABC"}}

	baseFactory := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kr).
		WithChainID("test-chain").
		WithGas(200000).
		WithGasPrices("0.01agxn").
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	s := New(log.NewNopLogger(), keyName, encCfg.TxConfig, ai, baseFactory, subClient)
	s.submit(context.Background(), oracletypes.OracleReport{
		RequestId: 1,
		RawData:   "1.23",
		Nonce:     1,
	})

	if got := ai.CurrentSequenceNumber(); got != 8 {
		t.Fatalf("expected sequence incremented to 8, got %d", got)
	}
	subClient.mu.Lock()
	defer subClient.mu.Unlock()
	if subClient.calls != 1 {
		t.Fatalf("expected BroadcastTx calls=1, got %d", subClient.calls)
	}
}

func TestSubmitter_Submit_Code32ResetsSequence(t *testing.T) {
	t.Parallel()
	setupBech32(t)

	encCfg := guruencoding.MakeConfig(guruconfig.GuruChainID)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	keyName := "k2"
	kr, addr := newTestKeyringAndAddress(t, encCfg.Codec, keyName)

	auth := &countingAuthClient{
		resps: []*authtypes.QueryAccountInfoResponse{
			{Info: &authtypes.BaseAccount{AccountNumber: 10, Sequence: 99}},
		},
	}
	ai := NewAccountInfo(auth, addr)
	atomic.StoreUint64(&ai.accountNumber, 10)
	atomic.StoreUint64(&ai.sequenceNumber, 7)

	subClient := &mockSubmitClient{resp: &sdk.TxResponse{Code: 32, TxHash: "ABC"}}

	baseFactory := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kr).
		WithChainID("test-chain").
		WithGas(200000).
		WithGasPrices("0.01agxn").
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	s := New(log.NewNopLogger(), keyName, encCfg.TxConfig, ai, baseFactory, subClient)
	s.submit(context.Background(), oracletypes.OracleReport{
		RequestId: 1,
		RawData:   "1.23",
		Nonce:     1,
	})

	if got := ai.CurrentSequenceNumber(); got != 99 {
		t.Fatalf("expected sequence reset to 99, got %d", got)
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if got := auth.calls; got != 1 {
		t.Fatalf("expected AccountInfo called once, got %d", got)
	}
}

func TestSubmitter_Start_ResetsAccountInfoOnce(t *testing.T) {
	t.Parallel()
	setupBech32(t)

	encCfg := guruencoding.MakeConfig(guruconfig.GuruChainID)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	keyName := "k3"
	kr, addr := newTestKeyringAndAddress(t, encCfg.Codec, keyName)

	auth := &countingAuthClient{
		resps: []*authtypes.QueryAccountInfoResponse{
			{Info: &authtypes.BaseAccount{AccountNumber: 1, Sequence: 2}},
		},
	}
	ai := NewAccountInfo(auth, addr)

	subClient := &mockSubmitClient{resp: &sdk.TxResponse{Code: 0, TxHash: "ABC"}}
	baseFactory := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kr).
		WithChainID("test-chain").
		WithGas(200000).
		WithGasPrices("0.01agxn").
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	s := New(log.NewNopLogger(), keyName, encCfg.TxConfig, ai, baseFactory, subClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan oracletypes.OracleReport)
	go s.Start(ctx, resultCh)

	// close channel to terminate Start loop quickly
	close(resultCh)

	deadline := time.After(2 * time.Second)
	for {
		if ai.AccountNumber() == 1 && ai.CurrentSequenceNumber() == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for initial ResetAccountInfo to take effect")
		case <-time.After(10 * time.Millisecond):
		}
	}

	auth.mu.Lock()
	defer auth.mu.Unlock()
	if auth.calls != 1 {
		t.Fatalf("expected ResetAccountInfo (AccountInfo RPC) calls=1, got %d", auth.calls)
	}
}

func TestSubmitter_Submit_BroadcastErrorDoesNotIncrementOrReset(t *testing.T) {
	t.Parallel()
	setupBech32(t)

	encCfg := guruencoding.MakeConfig(guruconfig.GuruChainID)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	keyName := "k4"
	kr, addr := newTestKeyringAndAddress(t, encCfg.Codec, keyName)

	auth := &countingAuthClient{}
	ai := NewAccountInfo(auth, addr)
	atomic.StoreUint64(&ai.accountNumber, 10)
	atomic.StoreUint64(&ai.sequenceNumber, 7)

	subClient := &mockSubmitClient{err: errors.New("broadcast failed")} // any non-nil error

	baseFactory := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kr).
		WithChainID("test-chain").
		WithGas(200000).
		WithGasPrices("0.01agxn").
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	s := New(log.NewNopLogger(), keyName, encCfg.TxConfig, ai, baseFactory, subClient)
	s.submit(context.Background(), oracletypes.OracleReport{
		RequestId: 2,
		RawData:   "1.23",
		Nonce:     1,
	})

	if got := ai.CurrentSequenceNumber(); got != 7 {
		t.Fatalf("expected sequence unchanged, got %d", got)
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if auth.calls != 0 {
		t.Fatalf("expected no ResetAccountInfo call, got %d", auth.calls)
	}
}

func TestSubmitter_Submit_Code18DoesNotIncrementOrReset(t *testing.T) {
	t.Parallel()
	setupBech32(t)

	encCfg := guruencoding.MakeConfig(guruconfig.GuruChainID)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	keyName := "k5"
	kr, addr := newTestKeyringAndAddress(t, encCfg.Codec, keyName)

	auth := &countingAuthClient{}
	ai := NewAccountInfo(auth, addr)
	atomic.StoreUint64(&ai.accountNumber, 10)
	atomic.StoreUint64(&ai.sequenceNumber, 7)

	subClient := &mockSubmitClient{resp: &sdk.TxResponse{Code: 18, TxHash: "ABC"}}

	baseFactory := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kr).
		WithChainID("test-chain").
		WithGas(200000).
		WithGasPrices("0.01agxn").
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	s := New(log.NewNopLogger(), keyName, encCfg.TxConfig, ai, baseFactory, subClient)
	s.submit(context.Background(), oracletypes.OracleReport{
		RequestId: 3,
		RawData:   "1.23",
		Nonce:     1,
	})

	if got := ai.CurrentSequenceNumber(); got != 7 {
		t.Fatalf("expected sequence unchanged, got %d", got)
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if auth.calls != 0 {
		t.Fatalf("expected no ResetAccountInfo call, got %d", auth.calls)
	}
}

func TestSubmitter_Submit_Code33ResetsSequence(t *testing.T) {
	t.Parallel()
	setupBech32(t)

	encCfg := guruencoding.MakeConfig(guruconfig.GuruChainID)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	keyName := "k6"
	kr, addr := newTestKeyringAndAddress(t, encCfg.Codec, keyName)

	auth := &countingAuthClient{
		resps: []*authtypes.QueryAccountInfoResponse{
			{Info: &authtypes.BaseAccount{AccountNumber: 10, Sequence: 123}},
		},
	}
	ai := NewAccountInfo(auth, addr)
	atomic.StoreUint64(&ai.accountNumber, 10)
	atomic.StoreUint64(&ai.sequenceNumber, 7)

	subClient := &mockSubmitClient{resp: &sdk.TxResponse{Code: 33, TxHash: "ABC"}}

	baseFactory := tx.Factory{}.
		WithTxConfig(encCfg.TxConfig).
		WithKeybase(kr).
		WithChainID("test-chain").
		WithGas(200000).
		WithGasPrices("0.01agxn").
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	s := New(log.NewNopLogger(), keyName, encCfg.TxConfig, ai, baseFactory, subClient)
	s.submit(context.Background(), oracletypes.OracleReport{
		RequestId: 4,
		RawData:   "1.23",
		Nonce:     1,
	})

	if got := ai.CurrentSequenceNumber(); got != 123 {
		t.Fatalf("expected sequence reset to 123, got %d", got)
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if got := auth.calls; got != 1 {
		t.Fatalf("expected AccountInfo called once, got %d", got)
	}
}
