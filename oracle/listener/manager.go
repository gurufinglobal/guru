package listener

import (
	"context"
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
	logger log.Logger
	states map[string]*subscriptionState // query -> state
}

func NewSubscriptionManager(logger log.Logger, queries ...string) *SubscriptionManager {
	states := make(map[string]*subscriptionState)
	for _, query := range queries {
		states[query] = newSubscriptionState(query)
	}

	return &SubscriptionManager{
		logger: logger,
		states: states,
	}
}

func (m *SubscriptionManager) Start(ctx context.Context, client SubscriptionClient, reqIDCh chan<- uint64) {
	for _, state := range m.states {
		if err := state.start(ctx, client, reqIDCh); err != nil {
			m.logger.Error("subscription start failed", "error", err, "subscriber", types.SubscriberName, "query", state.query)
		}
	}

	go func() {
		<-ctx.Done()
		for _, state := range m.states {
			if err := state.stop(client); err != nil {
				m.logger.Error("subscription stop failed", "error", err, "subscriber", types.SubscriberName, "query", state.query)
			}
		}
	}()

	go m.stateCheck(ctx, client, reqIDCh)
}

func (m *SubscriptionManager) stateCheck(ctx context.Context, client SubscriptionClient, reqIDCh chan<- uint64) {
	ticker := time.NewTicker(types.HealthCheckInterval)
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
