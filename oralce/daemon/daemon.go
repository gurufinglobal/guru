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
	"github.com/GPTx-global/guru-v2/oralce/subscriber"
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
	// ctx       context.Context
	logger    log.Logger
	fatalCh   chan error
	clientCtx client.Context

	monitor   *monitor.Monitor
	worker    *worker.WorkerPool
	submitter *submiter.Submitter

	subscriber *subscriber.Subscriber
}

// New creates and initializes a new Oracle daemon instance
// Sets up all necessary components including encoding, client context, and sub-services
func New(ctx context.Context) *Daemon {
	d := new(Daemon)
	d.logger = log.NewLogger(os.Stdout)
	d.fatalCh = make(chan error, 1)

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
	d.monitor = monitor.New(d.logger, ctx, d.clientCtx)
	d.worker = worker.New(d.logger, ctx)
	d.submitter = submiter.NewSubmitter(d.logger, ctx, d.clientCtx)
	d.logger.Info("daemon initialized")

	d.subscriber = subscriber.New(ctx, d.logger, d.clientCtx)

	return d
}

// Start initializes and begins all daemon operations
// Starts the WebSocket client, monitoring services, and background goroutines
func (d *Daemon) Start() {
	// Start the CometBFT WebSocket client
	if err := d.clientCtx.Client.(*comethttp.HTTP).Start(); err != nil {
		d.logger.Error("failed to start comet client", "error", err)
		// notify fatal to supervisor for restart
		select {
		case d.fatalCh <- fmt.Errorf("failed to start comet client: %w", err):
		default:
		}
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

// Fatal returns a channel that signals unrecoverable errors to a supervisor
func (d *Daemon) Fatal() <-chan error { return d.fatalCh }

// ensureConnection monitors WebSocket health and handles reconnection
// Runs in a separate goroutine to maintain reliable blockchain connectivity
func (d *Daemon) ensureConnection(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("root context done")
			return

		case <-ticker.C:
			// Check WebSocket health periodically
			if !d.isWebSocketHealthy() {
				d.logger.Error("websocket connection is not healthy")
				failures++

				// On too many failures, notify supervisor and exit
				if failures >= config.RetryMaxAttempts() {
					d.logger.Error("max failures reached, stopping daemon", "failures", failures)
					select {
					case d.fatalCh <- fmt.Errorf("websocket connection failed after %d attempts", failures):
					default:
					}
					return
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
		case <-d.ctx.Done():
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
		case <-d.ctx.Done():
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
	ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
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

func (d *Daemon) temp_loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("root context done")
			return

		case oracleEvent := <-d.subscriber.EventCh():
			switch event := oracleEvent.(type) {
			case error:
				d.logger.Error("error", "error", event)
				d.fatalCh <- event
				return

			case oracletypes.OracleRequestDoc:
				d.logger.Info("process request doc", "id", event.RequestId)
				d.worker.ProcessRequestDoc(event)

			case coretypes.ResultEvent:
				d.logger.Info("process complete", "id", event.Events[types.CompleteID], "nonce", event.Events[types.CompleteNonce])
				// Update gas prices from network events
				if gasPrices, ok := event.Events[types.MinGasPrice]; ok {
					for _, gasPrice := range gasPrices {
						config.SetGasPrice(gasPrice)
					}
				}

				// Process completion events to update Oracle job nonces
				for i, reqID := range event.Events[types.CompleteID] {
					nonce, err := strconv.ParseUint(event.Events[types.CompleteNonce][i], 10, 64)
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
}

func (d *Daemon) temp_healthcheck(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("root context done")
			return

		case <-ticker.C:
			if d.isWebSocketHealthy() { // 건강함
				failures = 0
				continue
			}

			failures++
			if failures <= 3 {
				continue
			}

			d.logger.Error("붕괴됨")
			d.fatalCh <- fmt.Errorf("websocket connection failed after %d attempts", failures)
			return
		}
	}
}
