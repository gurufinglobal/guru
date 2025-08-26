package subscriber

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

type Subscriber struct {
	logger  log.Logger
	eventCh chan any
}

// New creates a Subscriber, subscribes to required queries, and starts the event loop.
// It returns nil when subscriptions cannot be established.
func New(ctx context.Context, logger log.Logger, subsClient *http.HTTP, queryClient oracletypes.QueryClient) *Subscriber {
	s := &Subscriber{
		logger:  logger,
		eventCh: make(chan any, config.ChannelSize()),
	}

	registerCh, updateCh, completeCh := s.subscribeToEvents(ctx, subsClient)
	if registerCh == nil || updateCh == nil || completeCh == nil {
		s.logger.Error("subscribe streams failed")
		return nil
	}

	go s.runEventLoop(ctx, queryClient, registerCh, updateCh, completeCh)

	return s
}

// EventCh returns a read-only channel producing subscription events.
func (s *Subscriber) EventCh() <-chan any {
	return s.eventCh
}

// subscribeToEvents subscribes to register, update, and completion events.
// It automatically unsubscribes when the context is canceled.
func (s *Subscriber) subscribeToEvents(ctx context.Context, subsClient *http.HTTP) (<-chan coretypes.ResultEvent, <-chan coretypes.ResultEvent, <-chan coretypes.ResultEvent) {
	go func() {
		<-ctx.Done()
		subsClient.UnsubscribeAll(ctx, "")
		s.logger.Info("unsubscribed all")
	}()

	registerCh, err := subsClient.Subscribe(ctx, "", "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'", config.ChannelSize())
	if err != nil {
		s.logger.Error("subscribe register failed", "error", err)
		return nil, nil, nil
	}

	updateCh, err := subsClient.Subscribe(ctx, "", "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'", config.ChannelSize())
	if err != nil {
		s.logger.Error("subscribe update failed", "error", err)
		return nil, nil, nil
	}

	completeQuery := fmt.Sprintf("tm.event='NewBlock' AND %s.%s EXISTS", oracletypes.EventTypeCompleteOracleDataSet, oracletypes.AttributeKeyRequestId)
	completeCh, err := subsClient.Subscribe(ctx, "", completeQuery, config.ChannelSize())
	if err != nil {
		s.logger.Error("subscribe complete failed", "error", err)
		return nil, nil, nil
	}

	return registerCh, updateCh, completeCh
}

// runEventLoop boots current docs, then forwards subscription events to eventCh.
// It closes the output channel when the loop exits.
func (s *Subscriber) runEventLoop(ctx context.Context, queryClient oracletypes.QueryClient, registerCh <-chan coretypes.ResultEvent, updateCh <-chan coretypes.ResultEvent, completeCh <-chan coretypes.ResultEvent) {
	defer func() {
		close(s.eventCh)
		s.logger.Info("event monitor stopped")
	}()

	res, err := queryClient.OracleRequestDocs(ctx, &oracletypes.QueryOracleRequestDocsRequest{Status: oracletypes.RequestStatus_REQUEST_STATUS_ENABLED})
	if err != nil {
		s.logger.Debug("query request docs error", "error", err)
		s.eventCh <- err
		return
	}

	time.Sleep(time.Second * 5)

	for _, doc := range res.OracleRequestDocs {
		s.logger.Info("loaded request", "id", doc.RequestId, "nonce", doc.Nonce)
		s.eventCh <- *doc
	}

	s.logger.Info("event monitor started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("subscriber done")
			return

		case event := <-registerCh:
			requestId, err := parseRequestIDFromEvent(event, types.RegisterID)
			if err != nil {
				s.logger.Debug("register event invalid", "error", err)
				continue
			}

			queryRes, err := queryClient.OracleRequestDoc(ctx, &oracletypes.QueryOracleRequestDocRequest{RequestId: requestId})
			if err != nil {
				s.logger.Debug("query request doc error", "error", err, "request_id", requestId)
				continue
			}

			s.eventCh <- queryRes.RequestDoc
			s.logger.Info("request watch", "id", queryRes.RequestDoc.RequestId, "nonce", queryRes.RequestDoc.Nonce)

		case event := <-updateCh:
			requestId, err := parseRequestIDFromEvent(event, types.UpdateID)
			if err != nil {
				s.logger.Debug("update event invalid", "error", err)
				continue
			}

			queryRes, err := queryClient.OracleRequestDoc(ctx, &oracletypes.QueryOracleRequestDocRequest{RequestId: requestId})
			if err != nil {
				s.logger.Debug("query request doc error", "error", err, "request_id", requestId)
				continue
			}

			s.eventCh <- queryRes.RequestDoc
			s.logger.Info("update watch", "id", queryRes.RequestDoc.RequestId, "nonce", queryRes.RequestDoc.Nonce)

		case event := <-completeCh:
			if gasPrices, ok := event.Events[types.MinGasPrice]; ok {
				gasPrice := gasPrices[0]
				config.SetGasPrice(gasPrice)
				s.logger.Debug("gas price updated", "gas_price", gasPrice)
			}

			s.eventCh <- event
			s.logger.Info("complete watch", "id", event.Events[types.CompleteID][0], "nonce", event.Events[types.CompleteNonce][0])
		}
	}
}

// parseRequestIDFromEvent extracts a request ID by attribute key from the event map.
func parseRequestIDFromEvent(event coretypes.ResultEvent, eventType string) (uint64, error) {
	valsId, ok := event.Events[eventType]
	if !ok || len(valsId) == 0 {
		return 0, fmt.Errorf("event '%s' missing request id", eventType)
	}

	requestId, err := strconv.ParseUint(valsId[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid request id value for '%s': %w", eventType, err)
	}

	return requestId, nil
}
