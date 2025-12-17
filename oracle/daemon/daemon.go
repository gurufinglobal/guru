package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

	listener   *listener.SubscriptionManager
	aggregator *aggregator.Aggregator
	submitter  *submitter.Submitter

	oracleQueryClient    oracletypes.QueryClient
	feemarketQueryClient feemarkettypes.QueryClient
	denom                string
}

const (
	channelBuffer      = 16
	defaultHTTPTimeout = 30 * time.Second
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
	registry, err := provider.New(logger, categories.Categories,
		provider.NewCoinbaseProvider(httpClient),
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
		logger:      logger,
		clientCtx:   clientCtx,
		cometClient: cometClient,
		reqIDCh:     make(chan uint64, channelBuffer),
		taskCh:      make(chan types.OracleTask, channelBuffer),
		resultCh:    make(chan oracletypes.OracleReport, channelBuffer),
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
	go d.submitter.Start(ctx, d.resultCh)
	go d.aggregator.Start(ctx, d.taskCh, d.resultCh)

	d.listener.Start(ctx, d.clientCtx.Client.(*comethttp.HTTP), d.reqIDCh)

	go d.mainLoop(ctx)

	return nil
}

func (d *Daemon) mainLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.stop()
			return
		case reqID := <-d.reqIDCh:
			res, err := d.oracleQueryClient.OracleRequest(ctx, &oracletypes.QueryOracleRequestRequest{RequestId: reqID})
			if err != nil {
				d.logger.Error("get oracle request", "error", err, "request_id", reqID)
				continue
			}

			if res.Request.Category == oracletypes.Category_CATEGORY_OPERATION {
				go func() {
					feemarketRes, err := d.feemarketQueryClient.Params(ctx, &feemarkettypes.QueryParamsRequest{})
					if err != nil {
						d.logger.Error("get feemarket params", "error", err)
						return
					}

					d.submitter.UpdateGasPrice(feemarketRes.Params.MinGasPrice.Ceil().String() + d.denom)
				}()
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
