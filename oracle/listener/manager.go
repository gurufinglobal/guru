package listener

import (
	"context"
	"sync"
	"time"

	"cosmossdk.io/log"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/gurufinglobal/guru/v2/oracle/types"
)

type SubscriptionClient interface {
	IsRunning() bool
	Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (out <-chan coretypes.ResultEvent, err error)
	Unsubscribe(ctx context.Context, subscriber string, query string) error
}

type SubscriptionManager struct {
	logger              log.Logger
	states              map[string]*subscriptionState // query -> state
	healthCheckInterval time.Duration
	done                chan struct{}
	doneOnce            sync.Once
	startOnce           sync.Once
}

func NewSubscriptionManager(logger log.Logger, queries ...string) *SubscriptionManager {
	states := make(map[string]*subscriptionState)
	for _, query := range queries {
		states[query] = newSubscriptionState(query)
	}

	return &SubscriptionManager{
		logger:              logger,
		states:              states,
		healthCheckInterval: types.HealthCheckInterval,
		done:                make(chan struct{}),
	}
}

// SetHealthCheckInterval is intended for tests and advanced tuning.
// If d <= 0, the interval remains unchanged.
func (m *SubscriptionManager) SetHealthCheckInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	m.healthCheckInterval = d
}

func (m *SubscriptionManager) Start(ctx context.Context, client SubscriptionClient, reqIDCh chan<- uint64) {
	m.startOnce.Do(func() {
		var wg sync.WaitGroup
		wg.Add(2) // cleanup + stateCheck

		for _, state := range m.states {
			if err := state.start(ctx, client, reqIDCh); err != nil {
				m.logger.Error("subscription start failed", "error", err, "subscriber", types.SubscriberName, "query", state.query)
			}
		}

		go func() {
			defer wg.Done()
			<-ctx.Done()
			for _, state := range m.states {
				if err := state.stop(client); err != nil {
					m.logger.Error("subscription stop failed", "error", err, "subscriber", types.SubscriberName, "query", state.query)
				}
			}
		}()

		go func() {
			defer wg.Done()
			m.stateCheck(ctx, client, reqIDCh)
		}()

		go func() {
			wg.Wait()
			m.doneOnce.Do(func() { close(m.done) })
		}()
	})
}

func (m *SubscriptionManager) stateCheck(ctx context.Context, client SubscriptionClient, reqIDCh chan<- uint64) {
	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, state := range m.states {
				if state.isActive() {
					continue
				}

				if err := state.start(ctx, client, reqIDCh); err != nil {
					m.logger.Error("subscription start failed", "error", err, "subscriber", types.SubscriberName, "query", state.query)
				}
			}
		}
	}
}

// Done is closed when the manager has fully stopped (cleanup + stateCheck exit).
func (m *SubscriptionManager) Done() <-chan struct{} { return m.done }
