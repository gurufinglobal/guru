package daemon

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"cosmossdk.io/log"
	guruconfig "github.com/GPTx-global/guru-v2/cmd/gurud/config"
	"github.com/GPTx-global/guru-v2/encoding"
	"github.com/GPTx-global/guru-v2/oralce/config"
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
	"github.com/rs/zerolog"
)

// Daemon is the main Oracle service that coordinates all Oracle operations
// It manages blockchain connections, monitors events, processes data, and submits results
type Daemon struct {
	logger    log.Logger
	fatalCh   chan error
	clientCtx client.Context

	subscriber *subscriber.Subscriber
	worker     *worker.WorkerPool
	submitter  *submiter.Submitter
}

// New creates and initializes a new Oracle daemon instance
// Sets up all necessary components including encoding, client context, and sub-services
func New(ctx context.Context) *Daemon {
	d := new(Daemon)
	d.logger = log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	d.fatalCh = make(chan error, 1)

	encCfg := encoding.MakeConfig(guruconfig.GuruChainID)
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	cometClient, err := comethttp.New(config.ChainEndpoint(), "/websocket")
	if err != nil {
		d.logger.Error("create comet client", "error", err)
		return nil
	}

	if err := cometClient.Start(); err != nil {
		d.logger.Error("start comet client", "error", err)
		select {
		case d.fatalCh <- fmt.Errorf("failed to start comet client: %w", err):
		default:
		}
		return nil
	}
	d.logger.Info("comet client started", "endpoint", config.ChainEndpoint())

	d.clientCtx = client.Context{}.
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

	queryClient := oracletypes.NewQueryClient(d.clientCtx)
	d.subscriber = subscriber.New(ctx, d.logger, cometClient, queryClient)
	d.worker = worker.New(ctx, d.logger)
	d.submitter = submiter.NewSubmitter(d.logger, d.clientCtx)

	go d.ServeOracleResult(ctx)
	go d.runEventLoop(ctx, queryClient)
	go func() {
		<-ctx.Done()
		if cometClient.IsRunning() {
			cometClient.Stop()
		}
	}()

	d.logger.Info("daemon initialized")

	return d
}

// Fatal returns a channel that signals unrecoverable errors to a supervisor
func (d *Daemon) Fatal() <-chan error { return d.fatalCh }

func (d *Daemon) runEventLoop(ctx context.Context, queryClient oracletypes.QueryClient) {
	go d.runHealthcheck(ctx)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("event loop done")
			return

		case oracleEvent, ok := <-d.subscriber.EventCh():
			if !ok {
				d.logger.Info("subscriber closed")
				select {
				case d.fatalCh <- fmt.Errorf("subscriber closed"):
				default:
				}
				return
			}

			switch event := oracleEvent.(type) {
			case error:
				d.logger.Error("event error", "error", event)
				select {
				case d.fatalCh <- event:
				default:
				}
				return

			case oracletypes.OracleRequestDoc:
				timestamp := uint64(0)
				if event.Nonce != 0 {
					res, err := queryClient.OracleData(ctx, &oracletypes.QueryOracleDataRequest{RequestId: event.RequestId})
					if err != nil {
						d.logger.Error("query error", "error", err, "request_id", event.RequestId)
						continue
					}

					timestamp = res.DataSet.BlockTime
				}
				d.worker.ProcessRequestDoc(ctx, event, timestamp)

			case coretypes.ResultEvent:
				for i, reqID := range event.Events[types.CompleteID] {
					nonce, err := strconv.ParseUint(event.Events[types.CompleteNonce][i], 10, 64)
					if err != nil {
						d.logger.Error("parse nonce error", "error", err, "req_id", reqID)
						continue
					}

					timestamp, err := strconv.ParseUint(event.Events[types.CompleteTime][i], 10, 64)
					if err != nil {
						d.logger.Error("parse time error", "error", err, "req_id", reqID)
						continue
					}

					d.worker.ProcessComplete(ctx, reqID, nonce, timestamp)
				}
			}
		}
	}
}

// ServeOracleResult processes completed Oracle jobs and submits results to blockchain
// Runs in a separate goroutine to handle result submission asynchronously
func (d *Daemon) ServeOracleResult(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("ServeOracleResult context done")
			return

		case result, ok := <-d.worker.Results():
			if !ok {
				d.logger.Info("worker results channel closed")
				return
			}

			if result == nil {
				d.logger.Error("http client error")
				select {
				case d.fatalCh <- fmt.Errorf("oracle result is nil"):
				default:
				}
				return
			}
			d.logger.Info("submit result", "id", result.ID, "nonce", result.Nonce)
			d.submitter.BroadcastTxWithRetry(ctx, *result)
		}
	}
}

func (d *Daemon) runHealthcheck(ctx context.Context) {
	ticker := time.NewTicker(config.RetryMaxDelaySec())
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("healthcheck done")
			return

		case <-ticker.C:
			if d.isWebSocketHealthy(ctx) {
				failures = 0
				continue
			}

			failures++
			if failures <= config.RetryMaxAttempts() {
				d.logger.Info("websocket unhealthy", "attempt", failures)
				continue
			}

			d.logger.Error("websocket unhealthy limit reached")
			select {
			case d.fatalCh <- fmt.Errorf("websocket connection failed after %d attempts", failures):
			default:
			}
			return
		}
	}
}

// isWebSocketHealthy checks if WebSocket connection is working by attempting a lightweight operation
// Returns true if the WebSocket client is running and can successfully call Status API
func (d *Daemon) isWebSocketHealthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, config.RetryMaxDelaySec())
	defer cancel()

	client := d.clientCtx.Client.(*comethttp.HTTP)

	if !client.IsRunning() {
		d.logger.Info("websocket not running")
		return false
	}

	if _, err := client.Status(ctx); err != nil {
		d.logger.Debug("websocket status error", "error", err)
		return false
	}

	d.logger.Debug("websocket healthy")

	return true
}
