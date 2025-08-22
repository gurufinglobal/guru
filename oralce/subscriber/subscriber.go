package subscriber

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
)

type Subscriber struct {
	logger  log.Logger
	eventCh chan any
}

// New creates a Subscriber, initializes clients, subscribes to events,
// and starts the event loop. Returns nil if initialization fails.
func New(ctx context.Context, logger log.Logger, clientCtx client.Context) *Subscriber {
	s := &Subscriber{
		logger:  logger,
		eventCh: make(chan any, config.ChannelSize()),
	}

	subsClient, queryClient := s.initClients(clientCtx)
	if subsClient == nil || queryClient == nil {
		s.logger.Error("failed to initialize RPC and query clients")
		return nil
	}

	registerCh, updateCh, completeCh := s.subscribeToEvents(ctx, subsClient)
	if registerCh == nil || updateCh == nil || completeCh == nil {
		s.logger.Error("failed to subscribe to event streams")
		return nil
	}

	go s.runEventLoop(ctx, queryClient, registerCh, updateCh, completeCh)

	return s
}

// EventCh returns the read-only channel that delivers subscriber events.
func (s *Subscriber) EventCh() <-chan any {
	return s.eventCh
}

// initClients validates the RPC client and constructs an oracle query client.
// Returns nils if the RPC client is not running.
func (s *Subscriber) initClients(clientCtx client.Context) (*http.HTTP, oracletypes.QueryClient) {
	subsClient := clientCtx.Client.(*http.HTTP)
	if !subsClient.IsRunning() {
		s.logger.Error("tendermint RPC client is not running")
		return nil, nil
	}

	return subsClient, oracletypes.NewQueryClient(clientCtx)
}

// subscribeToEvents subscribes to register/update Tx events and the
// completion event emitted on new blocks. It auto-unsubscribes on ctx cancel.
func (s *Subscriber) subscribeToEvents(ctx context.Context, subsClient *http.HTTP) (<-chan coretypes.ResultEvent, <-chan coretypes.ResultEvent, <-chan coretypes.ResultEvent) {
	go func() {
		<-ctx.Done()
		subsClient.UnsubscribeAll(ctx, "")
		s.logger.Info("unsubscribed from all events")
	}()

	registerCh, err := subsClient.Subscribe(ctx, "", "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'", config.ChannelSize())
	if err != nil {
		s.logger.Error("subscribe to register events failed", "error", err)
		return nil, nil, nil
	}

	updateCh, err := subsClient.Subscribe(ctx, "", "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'", config.ChannelSize())
	if err != nil {
		s.logger.Error("subscribe to update events failed", "error", err)
		return nil, nil, nil
	}

	completeCh, err := subsClient.Subscribe(ctx, "", "tm.event='NewBlock' AND guru.oracle.v1.EventCompleteOracleDataSet EXISTS", config.ChannelSize())
	if err != nil {
		s.logger.Error("subscribe to completion events failed", "error", err)
		return nil, nil, nil
	}

	return registerCh, updateCh, completeCh
}

// runEventLoop bootstraps existing docs, then listens and dispatches
// incoming events onto the subscriber's channel. It closes the channel on exit.
func (s *Subscriber) runEventLoop(ctx context.Context, queryClient oracletypes.QueryClient, registerCh <-chan coretypes.ResultEvent, updateCh <-chan coretypes.ResultEvent, completeCh <-chan coretypes.ResultEvent) {
	defer func() {
		close(s.eventCh)
		s.logger.Info("event channel closed")
	}()

	res, err := queryClient.OracleRequestDocs(ctx, &oracletypes.QueryOracleRequestDocsRequest{Status: oracletypes.RequestStatus_REQUEST_STATUS_ENABLED})
	if err != nil {
		s.logger.Debug("query OracleRequestDocs failed", "error", err)
		s.eventCh <- err
		return
	}

	for _, doc := range res.OracleRequestDocs {
		s.logger.Debug("loaded existing request doc", "id", doc.RequestId)
		s.eventCh <- *doc
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("subscriber context done")
			return

		case event := <-registerCh:
			requestId, err := parseRequestIDFromEvent(event, types.RegisterID)
			if err != nil {
				s.logger.Debug("invalid register event: request id error", "error", err)
				continue
			}

			queryRes, err := queryClient.OracleRequestDoc(ctx, &oracletypes.QueryOracleRequestDocRequest{RequestId: requestId})
			if err != nil {
				s.logger.Debug("query OracleRequestDoc failed", "error", err, "request_id", requestId)
				continue
			}

			s.eventCh <- queryRes.RequestDoc

		case event := <-updateCh:
			requestId, err := parseRequestIDFromEvent(event, types.UpdateID)
			if err != nil {
				s.logger.Debug("invalid update event: request id error", "error", err)
				continue
			}

			queryRes, err := queryClient.OracleRequestDoc(ctx, &oracletypes.QueryOracleRequestDocRequest{RequestId: requestId})
			if err != nil {
				s.logger.Debug("query OracleRequestDoc failed", "error", err, "request_id", requestId)
				continue
			}

			s.eventCh <- queryRes.RequestDoc

		case event := <-completeCh:
			s.eventCh <- event
		}
	}
}

// parseRequestIDFromEvent extracts the request id value from the event by key.
// Returns an error if the field is missing or not a valid unsigned integer.
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
