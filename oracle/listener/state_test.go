package listener

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/gurufinglobal/guru/v2/oracle/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type mockSubscriptionClient struct {
	mu sync.Mutex

	running bool

	subscribeCalls int
	subscribeErr   error
	subscribeCh    chan coretypes.ResultEvent

	unsubscribeCalls []string
	unsubscribeErr   error
}

func (m *mockSubscriptionClient) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *mockSubscriptionClient) Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.subscribeCalls++
	if m.subscribeErr != nil {
		return nil, m.subscribeErr
	}
	if m.subscribeCh == nil {
		m.subscribeCh = make(chan coretypes.ResultEvent, 8)
	}
	return m.subscribeCh, nil
}

func (m *mockSubscriptionClient) Unsubscribe(ctx context.Context, subscriber string, query string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unsubscribeCalls = append(m.unsubscribeCalls, query)
	return m.unsubscribeErr
}

func TestSubscriptionState_StartStop_IdempotentAndUnsubscribe(t *testing.T) {
	t.Parallel()

	client := &mockSubscriptionClient{running: true}
	state := newSubscriptionState("q1")
	reqCh := make(chan uint64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := state.start(ctx, client, reqCh); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if !state.isActive() {
		t.Fatalf("expected active after start")
	}

	// stop should call unsubscribe
	if err := state.stop(client); err != nil {
		t.Fatalf("stop error: %v", err)
	}
	if state.isActive() {
		t.Fatalf("expected inactive after stop")
	}
	client.mu.Lock()
	if len(client.unsubscribeCalls) != 1 || client.unsubscribeCalls[0] != "q1" {
		client.mu.Unlock()
		t.Fatalf("expected unsubscribe called for q1, got %#v", client.unsubscribeCalls)
	}
	client.mu.Unlock()

	// second stop is idempotent
	if err := state.stop(client); err != nil {
		t.Fatalf("expected nil on second stop, got %v", err)
	}
}

func TestSubscriptionState_Start_SubscribeErrorDoesNotActivate(t *testing.T) {
	t.Parallel()

	client := &mockSubscriptionClient{running: true, subscribeErr: errors.New("subscribe failed")}
	state := newSubscriptionState("q1")
	reqCh := make(chan uint64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := state.start(ctx, client, reqCh); err == nil {
		t.Fatalf("expected error")
	}
	if state.isActive() {
		t.Fatalf("expected inactive after failed start")
	}
}

func TestSubscriptionState_EventLoop_EmitsRequestID(t *testing.T) {
	t.Parallel()

	client := &mockSubscriptionClient{running: true}
	state := newSubscriptionState("q1")
	reqCh := make(chan uint64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := state.start(ctx, client, reqCh); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer func() { _ = state.stop(client) }()

	eventKey := oracletypes.EventTypeOracleTask + "." + oracletypes.AttributeKeyRequestID
	client.subscribeCh <- coretypes.ResultEvent{
		Events: map[string][]string{eventKey: {"42"}},
	}

	select {
	case got := <-reqCh:
		if got != 42 {
			t.Fatalf("expected 42, got %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for request id")
	}
}

func TestSubscriptionManager_Start_CancelStopsAll(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	mgr := NewSubscriptionManager(logger, "q1", "q2")
	client := &mockSubscriptionClient{running: true}
	reqCh := make(chan uint64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	mgr.Start(ctx, client, reqCh)

	// cancel should trigger stop/unsubscribe for each query
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		client.mu.Lock()
		n := len(client.unsubscribeCalls)
		client.mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for unsubscribe, got %d", n)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestSubscriptionState_Stop_SkipsUnsubscribeWhenClientNotRunning(t *testing.T) {
	t.Parallel()

	client := &mockSubscriptionClient{running: false}
	state := newSubscriptionState("q1")
	reqCh := make(chan uint64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := state.start(ctx, client, reqCh); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if err := state.stop(client); err != nil {
		t.Fatalf("stop error: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.unsubscribeCalls) != 0 {
		t.Fatalf("expected no unsubscribe calls when client not running, got %#v", client.unsubscribeCalls)
	}
}

func TestSubscriptionState_Stop_UnsubscribeErrorPropagates(t *testing.T) {
	t.Parallel()

	client := &mockSubscriptionClient{running: true, unsubscribeErr: errors.New("unsubscribe failed")}
	state := newSubscriptionState("q1")
	reqCh := make(chan uint64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := state.start(ctx, client, reqCh); err != nil {
		t.Fatalf("start error: %v", err)
	}

	err := state.stop(client)
	if err == nil {
		t.Fatalf("expected unsubscribe error")
	}
}

func TestSubscriptionManager_StateCheck_RestartsInactive(t *testing.T) {
	t.Parallel()

	logger := log.NewNopLogger()
	mgr := NewSubscriptionManager(logger, "q1")
	mgr.SetHealthCheckInterval(10 * time.Millisecond)

	reqCh := make(chan uint64, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &mockSubscriptionClient{running: true}
	client.subscribeErr = errors.New("first subscribe fails")

	// Make first subscribe fail.
	mgr.Start(ctx, client, reqCh)

	// After a short delay, allow subscribe to succeed so healthcheck can restart.
	time.AfterFunc(30*time.Millisecond, func() {
		client.mu.Lock()
		client.subscribeErr = nil
		client.mu.Unlock()
	})

	deadline := time.After(500 * time.Millisecond)
	for {
		client.mu.Lock()
		calls := client.subscribeCalls
		client.mu.Unlock()
		if calls >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected Subscribe to be called at least twice, got %d", calls)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// ensure we reference constants used by listener package to avoid accidental drift.
var _ = types.SubscriberName
