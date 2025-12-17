package listener

import (
	"context"
	"fmt"
	"sync"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/gurufinglobal/guru/v2/oracle/types"
	"github.com/gurufinglobal/guru/v2/oracle/utils"
)

type subscriptionStatus uint8

const (
	subscriptionInactive subscriptionStatus = iota
	subscriptionStarting
	subscriptionActive
)

const unsubscribeTimeout = 5 * time.Second

type subscriptionState struct {
	query  string
	status subscriptionStatus
	cancel context.CancelFunc
	mux    sync.Mutex
}

func newSubscriptionState(query string) *subscriptionState {
	return &subscriptionState{
		query: query,
	}
}

func (s *subscriptionState) isActive() bool {
	s.mux.Lock()
	defer s.mux.Unlock()

	return s.status != subscriptionInactive
}

func (s *subscriptionState) start(ctx context.Context, client SubscriptionClient, reqIDCh chan<- uint64) error {
	subCtx, cancel := context.WithCancel(ctx)

	s.mux.Lock()
	if s.status != subscriptionInactive {
		s.mux.Unlock()
		cancel()
		return nil
	}
	s.status = subscriptionStarting
	s.cancel = cancel
	s.mux.Unlock()

	ch, err := client.Subscribe(subCtx, types.SubscriberName, s.query, types.ChannelBufferSize)
	if err != nil {
		cancel()
		s.mux.Lock()
		s.status = subscriptionInactive
		s.cancel = nil
		s.mux.Unlock()
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	s.mux.Lock()
	s.status = subscriptionActive
	s.mux.Unlock()

	go s.eventLoop(subCtx, ch, reqIDCh)

	return nil
}

func (s *subscriptionState) stop(client SubscriptionClient) error {
	s.mux.Lock()
	if s.status == subscriptionInactive {
		s.mux.Unlock()
		return nil
	}

	cancel := s.cancel
	s.status = subscriptionInactive
	s.cancel = nil
	s.mux.Unlock()

	if cancel != nil {
		cancel()
	}

	if client != nil && client.IsRunning() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), unsubscribeTimeout)
		defer cleanupCancel()
		if err := client.Unsubscribe(cleanupCtx, types.SubscriberName, s.query); err != nil {
			return fmt.Errorf("failed to unsubscribe: %w", err)
		}
	}

	return nil
}

func (s *subscriptionState) eventLoop(ctx context.Context, eventCh <-chan coretypes.ResultEvent, reqIDCh chan<- uint64) {
	defer func() {
		s.mux.Lock()
		s.status = subscriptionInactive
		s.cancel = nil
		s.mux.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			requestID, err := utils.EventToRequestID(event)
			if err != nil {
				continue
			}

			select {
			case reqIDCh <- requestID:
			case <-ctx.Done():
				return
			}
		}
	}
}
