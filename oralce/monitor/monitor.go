// Package monitor handles blockchain event monitoring for Oracle operations
// Subscribes to Oracle-related events and manages request document queries
package monitor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
)

// Monitor manages blockchain event subscriptions and Oracle request monitoring
// Provides event filtering and processing for Oracle daemon operations
type Monitor struct {
	logger    log.Logger
	baseCtx   context.Context
	clientCtx client.Context

	// Event subscriber identifiers
	registerSubscriber string
	updateSubscriber   string
	completeSubscriber string

	// Event query strings for filtering
	registerQuery string
	updateQuery   string
	completeQuery string

	// Event channels for receiving filtered events
	registerEventCh <-chan coretypes.ResultEvent
	updateEventCh   <-chan coretypes.ResultEvent
	completeEventCh <-chan coretypes.ResultEvent
}

// New creates a new Monitor instance with configured event queries
// Sets up subscription parameters for Oracle-related blockchain events
func New(logger log.Logger, baseCtx context.Context, clientCtx client.Context) *Monitor {
	return &Monitor{
		logger:    logger,
		baseCtx:   baseCtx,
		clientCtx: clientCtx,
		// Event subscription identifiers
		registerSubscriber: "register",
		updateSubscriber:   "update",
		completeSubscriber: "complete",
		// Event query filters for specific Oracle transactions
		registerQuery:   "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'",
		updateQuery:     "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'",
		completeQuery:   fmt.Sprintf("tm.event='NewBlock' AND %s.%s EXISTS", oracletypes.EventTypeCompleteOracleDataSet, oracletypes.AttributeKeyRequestId),
		registerEventCh: nil,
		updateEventCh:   nil,
		completeEventCh: nil,
	}
}

// Start establishes event subscriptions to the blockchain
// Creates channels for receiving Oracle registration, update, and completion events
func (m *Monitor) Start() {
	var (
		err    error
		client = m.clientCtx.Client.(*http.HTTP)
	)

	// Subscribe to Oracle request registration events
	m.registerEventCh, err = client.Subscribe(m.baseCtx, m.registerSubscriber, m.registerQuery, config.ChannelSize())
	if err != nil {
		m.logger.Error("failed to subscribe to register events", "error", err)
		panic(err)
	}

	// Subscribe to Oracle request update events
	m.updateEventCh, err = client.Subscribe(m.baseCtx, m.updateSubscriber, m.updateQuery, config.ChannelSize())
	if err != nil {
		m.logger.Error("failed to subscribe to update events", "error", err)
		panic(err)
	}

	// Subscribe to Oracle completion events
	m.completeEventCh, err = client.Subscribe(m.baseCtx, m.completeSubscriber, m.completeQuery, config.ChannelSize())
	if err != nil {
		m.logger.Error("failed to subscribe to complete events", "error", err)
		panic(err)
	}

	m.logger.Info("started monitoring oracle events")
}

// Stop terminates all event subscriptions and cleans up channels
// Gracefully unsubscribes from all Oracle event streams
func (m *Monitor) Stop() {
	client := m.clientCtx.Client.(*http.HTTP)

	// Unsubscribe from all Oracle event types
	client.Unsubscribe(m.baseCtx, m.registerSubscriber, m.registerQuery)
	client.Unsubscribe(m.baseCtx, m.updateSubscriber, m.updateQuery)
	client.Unsubscribe(m.baseCtx, m.completeSubscriber, m.completeQuery)

	// Clear event channels
	m.registerEventCh = nil
	m.updateEventCh = nil
	m.completeEventCh = nil

	m.logger.Info("stopped monitoring oracle events")
}

// LoadRegisteredRequestDocs queries the blockchain for all active Oracle requests
// Returns enabled Oracle request documents for processing on daemon startup
func (m *Monitor) LoadRegisteredRequestDocs() []*oracletypes.OracleRequestDoc {
	oracleQueryClient := oracletypes.NewQueryClient(m.clientCtx)

	// Query for all enabled Oracle request documents
	res, err := oracleQueryClient.OracleRequestDocs(m.baseCtx, &oracletypes.QueryOracleRequestDocsRequest{Status: oracletypes.RequestStatus_REQUEST_STATUS_ENABLED})
	if err != nil {
		m.logger.Error("failed to query oracle request docs", "error", err)
		return nil
	}

	return res.OracleRequestDocs
}

// Subscribe listens for Oracle events from subscribed channels
// Returns different event types or nil on timeout or context cancellation
func (m *Monitor) Subscribe() any {
	select {
	case <-m.baseCtx.Done():
		// Graceful shutdown
		return nil

	case <-time.After(1 * time.Second):
		// Timeout to prevent infinite blocking
		return nil

	case event := <-m.registerEventCh:
		if err := m.validateAccountList(event, types.RegisterAccountList); err != nil {
			m.logger.Info("failed to validate register event", "error", err)
			return nil
		}

		requestId, err := m.validateRequestID(event, types.RegisterID)
		if err != nil {
			m.logger.Info("failed to validate register event", "error", err)
			return nil
		}

		queryRes, err := m.queryOracleRequestDoc(requestId)
		if err != nil {
			m.logger.Error("failed to query oracle request doc", "error", err)
			return nil
		}

		return queryRes.RequestDoc

	case event := <-m.updateEventCh:
		requestId, err := m.validateRequestID(event, types.UpdateID)
		if err != nil {
			m.logger.Info("failed to validate update event", "error", err)
			return nil
		}

		queryRes, err := m.queryOracleRequestDoc(requestId)
		if err != nil {
			m.logger.Error("failed to query oracle request doc", "error", err)
			return nil
		}

		return queryRes.RequestDoc

	case event := <-m.completeEventCh:
		return event
	}
}

// validateRequestID extracts and validates request ID from blockchain events
// Returns the parsed request ID or error if validation fails
func (m *Monitor) validateRequestID(event coretypes.ResultEvent, eventType string) (uint64, error) {
	valsId, ok := event.Events[eventType]
	if !ok || len(valsId) == 0 {
		return 0, fmt.Errorf("event missing request id")
	}

	// Parse request ID from event data
	requestId, err := strconv.ParseUint(valsId[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse request id: %w", err)
	}

	return requestId, nil
}

// validateAccountList checks if current Oracle account is authorized for the request
// Returns error if account is not found in the request's account list
func (m *Monitor) validateAccountList(event coretypes.ResultEvent, eventType string) error {
	valsAccountList, ok := event.Events[eventType]
	if !ok || len(valsAccountList) == 0 {
		return fmt.Errorf("event missing account list")
	}

	// Check if current account is authorized for this Oracle request
	accountList := valsAccountList[0]
	if !strings.Contains(accountList, m.clientCtx.FromAddress.String()) {
		return fmt.Errorf("account list does not contain current account")
	}

	return nil
}

// queryOracleRequestDoc retrieves a specific Oracle request document by ID
// Used to get complete request details after receiving an event notification
func (m *Monitor) queryOracleRequestDoc(requestId uint64) (*oracletypes.QueryOracleRequestDocResponse, error) {
	oracleQueryClient := oracletypes.NewQueryClient(m.clientCtx)

	// Query for specific Oracle request document
	return oracleQueryClient.OracleRequestDoc(m.baseCtx, &oracletypes.QueryOracleRequestDocRequest{RequestId: requestId})
}
