package listener

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/log"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type MonitoringClient interface {
	IsRunning() bool
	Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (out <-chan coretypes.ResultEvent, err error)
	Unsubscribe(ctx context.Context, subscriber string, query string) error
}

type Listener struct {
	logger log.Logger
}

func New(logger log.Logger) *Listener {
	return &Listener{
		logger: logger,
	}
}

func (l *Listener) Start(ctx context.Context, client MonitoringClient, reqIDCh chan<- uint64) error {
	if client == nil {
		return fmt.Errorf("monitoring client is nil")
	}
	query := fmt.Sprintf("tm.event='Tx' AND message.action='/%s.EventTypeOracleTask'", oracletypes.ModuleName)
	subscriber := "oracle-listener"

	eventCh, err := client.Subscribe(ctx, subscriber, query)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	go l.listenLoop(ctx, client, subscriber, query, eventCh, reqIDCh)

	l.logger.Info("oracle listener started", "query", query)

	return nil
}

func (l *Listener) listenLoop(
	ctx context.Context,
	client MonitoringClient,
	subscriber string,
	query string,
	eventCh <-chan coretypes.ResultEvent,
	reqIDCh chan<- uint64,
) {
	defer func() {
		if client == nil || !client.IsRunning() {
			return
		}

		if err := client.Unsubscribe(ctx, subscriber, query); err != nil {
			l.logger.Error("failed to unsubscribe listener", "error", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			requestID, err := parseRequestIDFromEvent(event)
			if err != nil {
				l.logger.Error("failed to parse request ID", "error", err)
				continue
			}

			reqIDCh <- requestID
			l.logger.Debug("sent task", "request_id", requestID)
		}
	}
}

func parseRequestIDFromEvent(event coretypes.ResultEvent) (uint64, error) {
	valsId, ok := event.Events[oracletypes.EventTypeOracleTask]
	if !ok || len(valsId) == 0 {
		return 0, fmt.Errorf("event '%s' missing request id", oracletypes.EventTypeOracleTask)
	}

	requestID, err := strconv.ParseUint(valsId[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid request id value for '%s': %w", oracletypes.EventTypeOracleTask, err)
	}

	return requestID, nil
}
