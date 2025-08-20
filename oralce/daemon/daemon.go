// Package daemon provides the main Oracle daemon implementation
// It coordinates blockchain monitoring, data fetching, and transaction submission
package daemon

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"cosmossdk.io/log"
	guruconfig "github.com/GPTx-global/guru-v2/cmd/gurud/config"
	"github.com/GPTx-global/guru-v2/encoding"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/monitor"
	"github.com/GPTx-global/guru-v2/oralce/submiter"
	"github.com/GPTx-global/guru-v2/oralce/types"
	"github.com/GPTx-global/guru-v2/oralce/worker"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	comethttp "github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// Daemon is the main Oracle service that coordinates all Oracle operations
// It manages blockchain connections, monitors events, processes data, and submits results
type Daemon struct {
	logger    log.Logger
	rootCtx   context.Context
	clientCtx client.Context

	monitor   *monitor.Monitor
	worker    *worker.WorkerPool
	submitter *submiter.Submitter
}

// NewDaemon creates and initializes a new Oracle daemon instance
// Sets up all necessary components including encoding, client context, and sub-services
func NewDaemon(rootCtx context.Context) *Daemon {
	d := new(Daemon)
	d.logger = log.NewLogger(os.Stdout)
	d.rootCtx = rootCtx

	// Setup encoding configuration with required interface registrations
	encCfg := encoding.MakeConfig(guruconfig.GuruChainID)
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	// Create WebSocket client for real-time blockchain events
	cometClient, err := comethttp.New(config.ChainEndpoint(), "/websocket")
	if err != nil {
		d.logger.Error("failed to create comet client", "error", err)
		return nil
	}

	// Configure client context with all necessary components
	clientCtx := client.Context{}.
		WithCodec(encCfg.Codec).
		WithInterfaceRegistry(encCfg.InterfaceRegistry).
		WithTxConfig(encCfg.TxConfig).
		WithLegacyAmino(encCfg.Amino).
		WithKeyring(config.Keyring()).
		WithChainID(config.ChainID()).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithNodeURI(config.ChainEndpoint()).
		WithClient(cometClient).
		WithFromAddress(config.Address()).
		WithFromName(config.KeyName()).
		WithBroadcastMode(flags.BroadcastSync)

	// Initialize daemon components
	d.clientCtx = clientCtx
	d.monitor = monitor.New(d.logger, d.rootCtx, d.clientCtx)
	d.worker = worker.New(d.logger, d.rootCtx)
	d.submitter = submiter.NewSubmitter(d.logger, d.rootCtx, d.clientCtx)
	d.logger.Info("daemon initialized")

	return d
}

// Start initializes and begins all daemon operations
// Starts the WebSocket client, monitoring services, and background goroutines
func (d *Daemon) Start() {
	// Start the CometBFT WebSocket client
	if err := d.clientCtx.Client.(*comethttp.HTTP).Start(); err != nil {
		d.logger.Error("failed to start comet client", "error", err)
		return
	}

	// Start event monitoring
	d.monitor.Start()
	d.logger.Info("daemon started")

	// Start background services
	go d.Monitor()
	go d.ServeOracleResult()
	go d.ensureConnection()
}

// Stop gracefully shuts down all daemon operations
// Stops monitoring and closes the WebSocket connection
func (d *Daemon) Stop() {
	d.monitor.Stop()
	d.clientCtx.Client.(*comethttp.HTTP).Stop()
	d.logger.Info("daemon stopped")
}

// ensureConnection monitors WebSocket health and handles reconnection
// Runs in a separate goroutine to maintain reliable blockchain connectivity
func (d *Daemon) ensureConnection() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-d.rootCtx.Done():
			d.logger.Info("root context done")
			return

		case <-ticker.C:
			// Check WebSocket health periodically
			if !d.isWebSocketHealthy() {
				d.logger.Error("websocket connection is not healthy")
				failures++

				// Panic after max failures to trigger restart
				if failures >= config.RetryMaxAttempts() {
					d.logger.Error("max failures reached, stopping daemon", "failures", failures)
					panic("websocket connection failed") // TODO: 추후 수정
				}

				// Attempt to recreate the WebSocket connection
				if err := d.recreateWebSocketClient(); err != nil {
					d.logger.Error("failed to recreate websocket client", "error", err)

					// Exponential backoff before next attempt
					backoff := min(config.RetryInitialBackoffSec()<<failures, config.RetryMaxBackoffSec())
					d.logger.Info("retrying connection", "backoff_sec", backoff, "attempt", failures)
					time.Sleep(time.Duration(backoff) * time.Second)
					continue
				}

				d.logger.Info("websocket client recreated successfully", "attempt", failures)
			} else {
				// Reset failure counter on successful health check
				if failures > 0 {
					d.logger.Info("connection restored", "previous_failures", failures)
					failures = 0
				}
			}
		}
	}
}

// Monitor handles blockchain event monitoring and Oracle request processing
// Loads existing requests on startup and continuously processes new events
func (d *Daemon) Monitor() {
	// Load and process existing Oracle request documents on startup
	docs := d.monitor.LoadRegisteredRequestDocs()
	for _, doc := range docs {
		if slices.Contains(doc.AccountList, d.clientCtx.FromAddress.String()) {
			d.logger.Info("load registered request doc", "id", doc.RequestId)
			d.worker.ProcessRequestDoc(*doc)
		}
	}

	// Main event monitoring loop
	for {
		select {
		case <-d.rootCtx.Done():
			d.logger.Info("root context done")

		default:
			// continue
		}

		// Subscribe to Oracle events from the blockchain
		oracleEvent := d.monitor.Subscribe()
		if oracleEvent == nil {
			continue
		}

		// Process different types of Oracle events
		switch oracleEvent := oracleEvent.(type) {
		case oracletypes.OracleRequestDoc:
			d.logger.Info("process request doc", "id", oracleEvent.RequestId)
			d.worker.ProcessRequestDoc(oracleEvent)

		case coretypes.ResultEvent:
			// Update gas prices from network events
			if gasPrices, ok := oracleEvent.Events[types.MinGasPrice]; ok {
				for _, gasPrice := range gasPrices {
					config.SetGasPrice(gasPrice)
				}
			}

			// Process completion events to update Oracle job nonces
			for i, reqID := range oracleEvent.Events[types.CompleteID] {
				nonce, err := strconv.ParseUint(oracleEvent.Events[types.CompleteNonce][i], 10, 64)
				if err != nil {
					d.logger.Error("failed to parse nonce", "error", err, "req_id", reqID)
					continue
				}

				d.logger.Info("process complete", "id", reqID, "nonce", nonce)
				d.worker.ProcessComplete(reqID, nonce)
			}
		}
	}
}

// ServeOracleResult processes completed Oracle jobs and submits results to blockchain
// Runs in a separate goroutine to handle result submission asynchronously
func (d *Daemon) ServeOracleResult() {
	for {
		select {
		case <-d.rootCtx.Done():
			d.logger.Info("root context done")
			return

		case <-time.After(1 * time.Second):
			continue

		case result := <-d.worker.Results():
			// Submit Oracle result to blockchain via transaction
			d.logger.Info("submit oracle result", "id", result.ID, "nonce", result.Nonce, "data", result.Data)
			d.submitter.BroadcastTxWithRetry(*result)
		}
	}
}

// isWebSocketHealthy checks if WebSocket connection is working by attempting a lightweight operation
// Returns true if the WebSocket client is running and can successfully call Status API
func (d *Daemon) isWebSocketHealthy() bool {
	ctx, cancel := context.WithTimeout(d.rootCtx, 5*time.Second)
	defer cancel()

	client := d.clientCtx.Client.(*comethttp.HTTP)

	// Check if client is running
	if !client.IsRunning() {
		d.logger.Info("websocket client is not running")
		return false
	}

	// Test WebSocket connectivity with a simple Status call
	if _, err := client.Status(ctx); err != nil {
		d.logger.Info("websocket status call failed", "error", err)
		return false
	}

	return true
}

// recreateWebSocketClient completely recreates the WebSocket client connection
// Used when connection becomes unhealthy to establish a fresh connection
func (d *Daemon) recreateWebSocketClient() error {
	d.logger.Info("recreating websocket client")

	// Stop existing client if it's running
	if existingClient := d.clientCtx.Client.(*comethttp.HTTP); existingClient.IsRunning() {
		if err := existingClient.Stop(); err != nil {
			d.logger.Warn("error stopping existing client", "error", err)
		}
	}

	// Stop existing monitor to prevent event conflicts
	d.monitor.Stop()

	// Create a completely new WebSocket client
	newClient, err := comethttp.New(config.ChainEndpoint(), "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create new comet client: %w", err)
	}

	// Start the new client
	if err := newClient.Start(); err != nil {
		return fmt.Errorf("failed to start new comet client: %w", err)
	}

	// Update client context with new client
	d.clientCtx = d.clientCtx.WithClient(newClient)

	// Create and start new monitor with fresh client
	d.monitor = monitor.New(d.logger, d.rootCtx, d.clientCtx)
	d.monitor.Start()

	d.logger.Info("websocket client recreation completed")
	return nil
}
