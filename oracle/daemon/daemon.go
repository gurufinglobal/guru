package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"cosmossdk.io/log"
	comethttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
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
	feemarkettypes "github.com/gurufinglobal/guru/v2/x/feemarket/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
	"github.com/rs/zerolog"
)

type Daemon struct {
	logger      log.Logger
	clientCtx   client.Context
	cometClient *comethttp.HTTP

	reqIDCh  chan uint64
	taskCh   chan types.OracleTask
	resultCh chan oracletypes.OracleReport

	listener   *listener.Listener
	aggregator *aggregator.Aggregator
	submitter  *submitter.Submitter

	authQueryClient   authtypes.QueryClient
	oracleQueryClient oracletypes.QueryClient
}

const (
	channelBuffer      = 16
	defaultHTTPTimeout = 30 * time.Second
)

func New(cfg *config.Config) (*Daemon, error) {
	logger := newLogger()
	encCfg := newEncodingConfig()

	cometClient, err := newCometClient(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("create comet client: %w", err)
	}

	kr, address, err := newKeyring(cfg, encCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("init keyring: %w", err)
	}

	clientCtx := newClientContext(encCfg, cfg, cometClient, kr, address)

	ctx := context.Background()
	oracleQueryClient := oracletypes.NewQueryClient(clientCtx)
	categories, err := oracleQueryClient.Categories(ctx, &oracletypes.QueryCategoriesRequest{})
	if err != nil {
		logger.Error("get categories", "error", err)
		return nil, fmt.Errorf("query categories: %w", err)
	}

	httpClient := &http.Client{Timeout: defaultHTTPTimeout}
	registry := provider.New(logger, categories.Categories,
		provider.NewCoinbaseProvider(httpClient),
	)

	authQueryClient := authtypes.NewQueryClient(clientCtx)
	accountInfo, err := fetchAccountInfo(ctx, authQueryClient, address, logger)
	if err != nil {
		return nil, fmt.Errorf("fetch account info: %w", err)
	}

	baseFactory, err := buildTxFactory(ctx, clientCtx, cfg, accountInfo, logger)
	if err != nil {
		return nil, fmt.Errorf("build tx factory: %w", err)
	}

	return &Daemon{
		logger:            logger,
		clientCtx:         clientCtx,
		cometClient:       cometClient,
		reqIDCh:           make(chan uint64, channelBuffer),
		taskCh:            make(chan types.OracleTask, channelBuffer),
		resultCh:          make(chan oracletypes.OracleReport, channelBuffer),
		listener:          listener.New(logger),
		aggregator:        aggregator.NewAggregator(logger, registry),
		submitter:         submitter.New(logger, cfg.Keyring.Name, address.String(), clientCtx.TxConfig, accountInfo, baseFactory, clientCtx),
		authQueryClient:   authQueryClient,
		oracleQueryClient: oracleQueryClient,
	}, nil
}

func (d *Daemon) Start(ctx context.Context) error {
	go d.submitter.Start(ctx, d.resultCh)
	go d.aggregator.Start(ctx, d.taskCh, d.resultCh)

	if err := d.listener.Start(ctx, d.clientCtx.Client.(*comethttp.HTTP), d.reqIDCh); err != nil {
		d.logger.Error("start listener", "error", err)
		return fmt.Errorf("start listener: %w", err)
	}

	go d.mainLoop(ctx)

	go func() {
		<-ctx.Done()
		d.stop()
	}()

	return nil
}

func (d *Daemon) mainLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case reqID := <-d.reqIDCh:
			res, err := d.oracleQueryClient.OracleRequest(ctx, &oracletypes.QueryOracleRequestRequest{RequestId: reqID})
			if err != nil {
				d.logger.Error("get oracle request", "error", err, "request_id", reqID)
				continue
			}

			d.taskCh <- types.OracleTask{
				Id:       res.Request.Id,
				Category: int32(res.Request.Category),
				Symbol:   res.Request.Symbol,
				Nonce:    res.Request.Nonce,
			}
		}
	}
}

func (d *Daemon) stop() {
	if d.cometClient == nil || !d.cometClient.IsRunning() {
		return
	}

	if err := d.cometClient.Stop(); err != nil {
		d.logger.Error("stop comet client", "error", err)
	}
}

func newLogger() log.Logger {
	return log.NewLogger(
		os.Stdout,
		log.LevelOption(zerolog.DebugLevel),
		log.TimeFormatOption(time.RFC3339),
		log.OutputJSONOption(),
	)
}

func newEncodingConfig() sdktestutil.TestEncodingConfig {
	encCfg := encoding.MakeConfig(guruconfig.GuruChainID)
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	return encCfg
}

func newCometClient(cfg *config.Config, logger log.Logger) (*comethttp.HTTP, error) {
	cometClient, err := comethttp.New(cfg.Chain.Endpoint, "/websocket")
	if err != nil {
		logger.Error("create comet client", "error", err)
		return nil, err
	}

	if err := cometClient.Start(); err != nil {
		logger.Error("start comet client", "error", err)
		return nil, err
	}

	return cometClient, nil
}

func newKeyring(cfg *config.Config, encCfg sdktestutil.TestEncodingConfig, logger log.Logger) (keyring.Keyring, sdk.AccAddress, error) {
	kr, err := keyring.New("guru", cfg.Keyring.Backend, cfg.Home, nil, encCfg.Codec, hd.EthSecp256k1Option())
	if err != nil {
		logger.Error("create keyring", "error", err)
		return nil, nil, err
	}

	info, err := kr.Key(cfg.Keyring.Name)
	if err != nil {
		logger.Error("get key info", "error", err)
		return nil, nil, err
	}

	address, err := info.GetAddress()
	if err != nil {
		logger.Error("get address", "error", err)
		return nil, nil, err
	}

	return kr, address, nil
}

func newClientContext(
	encCfg sdktestutil.TestEncodingConfig,
	cfg *config.Config,
	cometClient *comethttp.HTTP,
	kr keyring.Keyring,
	address sdk.AccAddress,
) client.Context {
	return client.Context{}.
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
}

func fetchAccountInfo(ctx context.Context, authClient authtypes.QueryClient, address sdk.AccAddress, logger log.Logger) (*submitter.AccountInfo, error) {
	res, err := authClient.AccountInfo(ctx, &authtypes.QueryAccountInfoRequest{Address: address.String()})
	if err != nil {
		logger.Error("get account info", "error", err)
		return nil, err
	}

	return submitter.NewAccountInfo(res.Info.AccountNumber, res.Info.Sequence), nil
}

func buildTxFactory(
	ctx context.Context,
	clientCtx client.Context,
	cfg *config.Config,
	accountInfo *submitter.AccountInfo,
	logger log.Logger,
) (tx.Factory, error) {
	feemarketQueryClient := feemarkettypes.NewQueryClient(clientCtx)
	feemarketRes, err := feemarketQueryClient.Params(ctx, &feemarkettypes.QueryParamsRequest{})
	if err != nil {
		logger.Error("get feemarket params", "error", err)
		return tx.Factory{}, err
	}

	gasPrice := feemarketRes.Params.MinGasPrice.Ceil().String() + guruconfig.BaseDenom

	baseFactory := tx.Factory{}.
		WithTxConfig(clientCtx.TxConfig).
		WithAccountRetriever(clientCtx.AccountRetriever).
		WithKeybase(clientCtx.Keyring).
		WithChainID(cfg.Chain.ChainID).
		WithGas(cfg.Gas.Limit).
		WithGasAdjustment(cfg.Gas.Adjustment).
		WithGasPrices(gasPrice).
		WithAccountNumber(accountInfo.AccountNumber()).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	return baseFactory, nil
}
