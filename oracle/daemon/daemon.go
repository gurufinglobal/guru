package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/log"
	comethttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	guruconfig "github.com/gurufinglobal/guru/v2/cmd/gurud/config"
	"github.com/gurufinglobal/guru/v2/crypto/hd"
	"github.com/gurufinglobal/guru/v2/encoding"
	"github.com/gurufinglobal/guru/v2/oracle/aggregator"
	"github.com/gurufinglobal/guru/v2/oracle/config"
	"github.com/gurufinglobal/guru/v2/oracle/listener"
	"github.com/gurufinglobal/guru/v2/oracle/provider"
	"github.com/gurufinglobal/guru/v2/oracle/submitter"
	"github.com/gurufinglobal/guru/v2/oracle/types"
	"github.com/gurufinglobal/guru/v2/oracle/utils"
	feemarkettypes "github.com/gurufinglobal/guru/v2/x/feemarket/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
	"github.com/rs/zerolog"
)

type Daemon struct {
	cfg     *config.Config
	homeDir string

	logger      log.Logger
	clientCtx   client.Context
	cometClient *comethttp.HTTP

	providerHTTPClient *http.Client
	providers          []provider.Provider
	pvRegistry         *provider.Registry
	baseFactory        tx.Factory
	gasPrice           string
	gasPriceMu         sync.RWMutex

	reqIDCh  chan uint64
	taskCh   chan types.OracleTask
	resultCh chan oracletypes.OracleReport

	listener   *listener.SubscriptionManager
	aggregator *aggregator.Aggregator
	submitter  *submitter.Submitter

	oracleQueryClient    oracletypes.QueryClient
	feemarketQueryClient feemarkettypes.QueryClient
	denom                string

	runMu     sync.Mutex
	runCancel context.CancelFunc
	runDone   chan struct{}

	healthMu sync.Mutex
}

const (
	channelBuffer       = 16
	defaultHTTPTimeout  = 30 * time.Second
	healthCheckInterval = 30 * time.Second
	healthCheckTimeout  = 5 * time.Second
	shutdownTimeout     = 10 * time.Second
)

func New(cfg *config.Config, homeDir string) (*Daemon, error) {
	if strings.TrimSpace(homeDir) == "" {
		return nil, fmt.Errorf("home directory is empty")
	}

	logger := log.NewLogger(
		os.Stdout,
		log.LevelOption(zerolog.DebugLevel),
		log.TimeFormatOption(time.RFC3339),
		log.OutputJSONOption(),
	)

	encCfg := encoding.MakeConfig(guruconfig.GuruChainID)
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	cometClient, err := comethttp.New(cfg.Chain.Endpoint, "/websocket")
	if err != nil {
		return nil, fmt.Errorf("create comet client: %w", err)
	}

	if err := cometClient.Start(); err != nil {
		return nil, fmt.Errorf("start comet client: %w", err)
	}

	var krIn io.Reader
	if cfg.Keyring.Passphrase != "" {
		// Provide passphrase twice (some backends may prompt for confirmation).
		krIn = strings.NewReader(cfg.Keyring.Passphrase + "\n" + cfg.Keyring.Passphrase + "\n")
	}
	kr, err := keyring.New("guru", cfg.Keyring.Backend, homeDir, krIn, encCfg.Codec, hd.EthSecp256k1Option())
	if err != nil {
		return nil, fmt.Errorf("create keyring: %w", err)
	}

	info, err := kr.Key(cfg.Keyring.Name)
	if err != nil {
		return nil, fmt.Errorf("get key info: %w", err)
	}

	address, err := info.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("get address: %w", err)
	}

	clientCtx := client.Context{}.
		WithCodec(encCfg.Codec).
		WithInterfaceRegistry(encCfg.InterfaceRegistry).
		WithTxConfig(encCfg.TxConfig).
		WithLegacyAmino(encCfg.Amino).
		WithKeyring(kr).
		WithChainID(cfg.Chain.ChainID).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithNodeURI(cfg.Chain.Endpoint).
		WithClient(cometClient).
		WithFromAddress(address).
		WithFromName(cfg.Keyring.Name).
		WithBroadcastMode(flags.BroadcastSync)

	ctx := context.Background()
	oracleQueryClient := oracletypes.NewQueryClient(clientCtx)
	categories, err := oracleQueryClient.Categories(ctx, &oracletypes.QueryCategoriesRequest{})
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}

	httpClient := &http.Client{Timeout: defaultHTTPTimeout}
	coinbase := provider.NewCoinbaseProvider(httpClient)
	registry, err := provider.New(logger, categories.Categories,
		coinbase,
	)
	if err != nil {
		return nil, fmt.Errorf("init provider registry: %w", err)
	}

	accountInfo := submitter.NewAccountInfo(authtypes.NewQueryClient(clientCtx), address)

	feemarketQueryClient := feemarkettypes.NewQueryClient(clientCtx)
	feemarketRes, err := feemarketQueryClient.Params(ctx, &feemarkettypes.QueryParamsRequest{})
	if err != nil {
		return nil, fmt.Errorf("get feemarket params: %w", err)
	}

	gasPrice := feemarketRes.Params.MinGasPrice.Ceil().String() + cfg.Gas.Denom

	baseFactory := tx.Factory{}.
		WithTxConfig(clientCtx.TxConfig).
		WithAccountRetriever(clientCtx.AccountRetriever).
		WithKeybase(clientCtx.Keyring).
		WithChainID(cfg.Chain.ChainID).
		WithGas(cfg.Gas.Limit).
		WithGasAdjustment(cfg.Gas.Adjustment).
		WithGasPrices(gasPrice).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	return &Daemon{
		cfg:     cfg,
		homeDir: homeDir,

		logger:             logger,
		clientCtx:          clientCtx,
		cometClient:        cometClient,
		providerHTTPClient: httpClient,
		providers:          []provider.Provider{coinbase},
		pvRegistry:         registry,
		baseFactory:        baseFactory,
		gasPrice:           gasPrice,

		reqIDCh:  make(chan uint64, channelBuffer),
		taskCh:   make(chan types.OracleTask, channelBuffer),
		resultCh: make(chan oracletypes.OracleReport, channelBuffer),
		listener: listener.NewSubscriptionManager(logger,
			types.OracleTaskIDQuery,
		),
		aggregator:           aggregator.NewAggregator(logger, registry),
		submitter:            submitter.New(logger, cfg.Keyring.Name, clientCtx.TxConfig, accountInfo, baseFactory, clientCtx),
		oracleQueryClient:    oracleQueryClient,
		feemarketQueryClient: feemarketQueryClient,
		denom:                cfg.Gas.Denom,
	}, nil
}

func (d *Daemon) Start(ctx context.Context) error {
	d.startRun(ctx)
	go d.healthLoop(ctx)

	go func() {
		<-ctx.Done()
		// Ensure we don't race with an in-flight restart.
		d.healthMu.Lock()
		defer d.healthMu.Unlock()
		d.stopRun()
		d.stopComet()
	}()
	return nil
}

func (d *Daemon) startRun(parent context.Context) {
	runCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})

	// Fresh channels per run to avoid cross-run mixing.
	reqIDCh := make(chan uint64, channelBuffer)
	taskCh := make(chan types.OracleTask, channelBuffer)
	resultCh := make(chan oracletypes.OracleReport, channelBuffer)

	// Rebuild components that depend on clientCtx/comet.
	accountInfo := submitter.NewAccountInfo(authtypes.NewQueryClient(d.clientCtx), d.clientCtx.GetFromAddress())
	baseFactory := d.baseFactory.WithGasPrices(d.currentGasPrice())

	d.runMu.Lock()
	d.runCancel = cancel
	d.runDone = done
	d.reqIDCh = reqIDCh
	d.taskCh = taskCh
	d.resultCh = resultCh
	d.listener = listener.NewSubscriptionManager(d.logger, types.OracleTaskIDQuery)
	d.aggregator = aggregator.NewAggregator(d.logger, d.pvRegistry)
	d.submitter = submitter.New(d.logger, d.cfg.Keyring.Name, d.clientCtx.TxConfig, accountInfo, baseFactory, d.clientCtx)
	d.runMu.Unlock()

	sub := d.submitter
	agg := d.aggregator
	lst := d.listener
	oqc := d.oracleQueryClient
	fqc := d.feemarketQueryClient
	denom := d.denom
	mainDone := make(chan struct{})
	sub.Start(runCtx, resultCh)
	agg.Start(runCtx, taskCh, resultCh)
	go func() {
		defer close(mainDone)
		d.mainLoop(runCtx, reqIDCh, taskCh, oqc, fqc, denom, sub)
	}()

	// Listener manages its own goroutines; cancellation is sufficient.
	lst.Start(runCtx, d.cometClient, reqIDCh)

	go func() {
		<-sub.Done()
		<-agg.Done()
		<-lst.Done()
		<-mainDone
		close(done)
	}()
}

func (d *Daemon) stopRun() {
	d.runMu.Lock()
	cancel := d.runCancel
	done := d.runDone
	d.runMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(shutdownTimeout):
			d.logger.Error("run shutdown timed out")
		}
	}
}

func (d *Daemon) mainLoop(
	ctx context.Context,
	reqIDCh <-chan uint64,
	taskCh chan<- types.OracleTask,
	oracleQueryClient oracletypes.QueryClient,
	feemarketQueryClient feemarkettypes.QueryClient,
	denom string,
	sub *submitter.Submitter,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case reqID := <-reqIDCh:
			res, err := oracleQueryClient.OracleRequest(ctx, &oracletypes.QueryOracleRequestRequest{RequestId: reqID})
			if err != nil {
				d.logger.Error("get oracle request", "error", err, "request_id", reqID)
				continue
			}

			// if res.Request.Category == oracletypes.Category_CATEGORY_OPERATION {
			// 	go func() {
			// 		feemarketRes, err := feemarketQueryClient.Params(ctx, &feemarkettypes.QueryParamsRequest{})
			// 		if err != nil {
			// 			d.logger.Error("get feemarket params", "error", err)
			// 			return
			// 		}

			// 		gasPrice := feemarketRes.Params.MinGasPrice.Ceil().String() + denom
			// 		d.setGasPrice(gasPrice)
			// 		sub.UpdateGasPrice(gasPrice)
			// 	}()
			// }

			taskCh <- types.OracleTask{
				Id:       res.Request.Id,
				Category: int32(res.Request.Category),
				Symbol:   res.Request.Symbol,
				Nonce:    res.Request.Nonce,
			}
		}
	}
}

func (d *Daemon) stopComet() {
	if d.cometClient == nil || !d.cometClient.IsRunning() {
		return
	}

	if err := d.cometClient.Stop(); err != nil {
		d.logger.Error("stop comet client", "error", err)
	}
}

func (d *Daemon) healthLoop(ctx context.Context) {
	nextWait := healthCheckInterval
	count, reset, inc := utils.TrackFailStreak()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(nextWait):
		}

		comet := d.healthCometTarget()
		if d.isCometHealthy(ctx, comet) {
			reset() // reset fail streak on recovery
			nextWait = healthCheckInterval
			continue
		}

		inc() // increment fail streak
		if count() < 3 {
			nextWait = 1 << count() * time.Second
			continue
		}

		d.logger.Error("comet health check failed repeatedly; restarting daemon components",
			"failures", count(),
		)
		d.restart(ctx)
		reset()
		nextWait = healthCheckInterval
	}
}

func (d *Daemon) healthCometTarget() *comethttp.HTTP {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()
	return d.cometClient
}

func (d *Daemon) isCometHealthy(ctx context.Context, comet *comethttp.HTTP) bool {
	if comet == nil || !comet.IsRunning() {
		return false
	}
	hctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	_, err := comet.Status(hctx)
	return err == nil
}

func (d *Daemon) restart(ctx context.Context) {
	d.healthMu.Lock()
	defer d.healthMu.Unlock()

	// Stop current run (components) first.
	d.stopRun()

	// Restart clients.
	d.stopComet()

	cometClient, err := comethttp.New(d.cfg.Chain.Endpoint, "/websocket")
	if err != nil {
		d.logger.Error("create comet client failed", "error", err)
		return
	}
	if err := cometClient.Start(); err != nil {
		d.logger.Error("start comet client failed", "error", err)
		return
	}
	d.cometClient = cometClient

	// Update client context and query clients.
	d.clientCtx = d.clientCtx.WithClient(cometClient)
	d.oracleQueryClient = oracletypes.NewQueryClient(d.clientCtx)
	d.feemarketQueryClient = feemarkettypes.NewQueryClient(d.clientCtx)

	// Restart provider HTTP client (best-effort).
	d.providerHTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	for _, pv := range d.providers {
		pv.SetHTTPClient(d.providerHTTPClient)
	}

	// Start a new run with fresh component instances.
	if ctx.Err() != nil {
		return
	}
	d.startRun(ctx)
}

func (d *Daemon) currentGasPrice() string {
	d.gasPriceMu.RLock()
	defer d.gasPriceMu.RUnlock()
	return d.gasPrice
}

func (d *Daemon) setGasPrice(v string) {
	d.gasPriceMu.Lock()
	d.gasPrice = v
	d.gasPriceMu.Unlock()
}
