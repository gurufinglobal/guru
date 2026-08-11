// Package cmd composes the gurud operator command tree.
package cmd

import (
	"crypto/tls"
	"fmt"
	"os"

	confixcmd "cosmossdk.io/tools/confix/cmd"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtcli "github.com/cometbft/cometbft/libs/cli"
	"github.com/cosmos/cosmos-sdk/client"
	clientcfg "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server"
	authtxconfig "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	cosmosevmcmd "github.com/cosmos/evm/client"
	evmdebug "github.com/cosmos/evm/client/debug"
	cosmosevmconfig "github.com/cosmos/evm/config"
	"github.com/cosmos/evm/crypto/hd"
	cosmosevmserver "github.com/cosmos/evm/server"
	srvflags "github.com/cosmos/evm/server/flags"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gurufinglobal/guru/v2/app"
	chainconfig "github.com/gurufinglobal/guru/v2/config"
)

// NewRootCmd creates the complete operator and client command surface without
// constructing a temporary App. Cosmos EVM's chain configuration is
// process-wide, so the selected command must be the sole App constructor in a
// gurud process.
func NewRootCmd() (*cobra.Command, error) {
	if err := chainconfig.SetupSDKConfig(); err != nil {
		return nil, err
	}
	encodingConfig, err := app.MakeEncodingConfig()
	if err != nil {
		return nil, err
	}
	defaultHome, err := chainconfig.DefaultNodeHome()
	if err != nil {
		return nil, err
	}

	initialClientContext := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.LegacyAmino).
		WithInput(os.Stdin).
		WithOutput(os.Stdout).
		WithAccountRetriever(types.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastSync).
		WithHomeDir(defaultHome).
		WithChainID(chainconfig.LocalChainID).
		WithViper(chainconfig.EnvPrefix).
		WithKeyringOptions(hd.EthSecp256k1Option()).
		WithLedgerHasProtobuf(true)
	// ReadPersistentCommandFlags resolves Ledger keys before the fully configured
	// client context is available. Advertise textual support during that first
	// pass, then replace this bootstrap querier with the live client below.
	bootstrapTextualTxConfig, err := app.NewTextualTxConfig(
		authtxconfig.NewGRPCCoinMetadataQueryFn(initialClientContext),
	)
	if err != nil {
		return nil, err
	}
	initialClientContext = initialClientContext.WithTxConfig(bootstrapTextualTxConfig)

	rootCommand := &cobra.Command{
		Use:           chainconfig.BinaryName,
		Short:         "Guru Cosmos EVM node",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          client.ValidateCmd,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			command.SetOut(command.OutOrStdout())
			command.SetErr(command.ErrOrStderr())

			commandContext := initialClientContext.
				WithCmdContext(command.Context()).
				WithInput(command.InOrStdin()).
				WithOutput(command.OutOrStdout())
			commandContext, err = client.ReadPersistentCommandFlags(commandContext, command.Flags())
			if err != nil {
				return err
			}
			commandContext, err = setGRPCClientWithAppCodec(command, commandContext, encodingConfig.InterfaceRegistry)
			if err != nil {
				return err
			}
			commandContext, err = clientcfg.ReadFromClientConfig(commandContext)
			if err != nil {
				return err
			}
			offline := commandContext.Offline
			if offlineFlag := command.Flags().Lookup(flags.FlagOffline); offlineFlag != nil && offlineFlag.Changed {
				offline, err = command.Flags().GetBool(flags.FlagOffline)
				if err != nil {
					return err
				}
				commandContext = commandContext.WithOffline(offline)
			}
			signMode := commandContext.SignModeStr
			if signModeFlag := command.Flags().Lookup(flags.FlagSignMode); signModeFlag != nil && signModeFlag.Changed {
				signMode, err = command.Flags().GetString(flags.FlagSignMode)
				if err != nil {
					return err
				}
			}
			if offline {
				if signMode == flags.SignModeTextual {
					return fmt.Errorf("SIGN_MODE_TEXTUAL requires an online client")
				}
				commandContext = commandContext.WithTxConfig(encodingConfig.TxConfig)
			} else {
				textualTxConfig, err := app.NewTextualTxConfig(
					authtxconfig.NewGRPCCoinMetadataQueryFn(commandContext),
				)
				if err != nil {
					return err
				}
				commandContext = commandContext.WithTxConfig(textualTxConfig)
			}
			if err := client.SetCmdClientContextHandler(commandContext, command); err != nil {
				return err
			}

			appTemplate, appConfig := newDefaultAppConfig()
			return server.InterceptConfigsPreRunHandler(
				command,
				appTemplate,
				appConfig,
				cmtcfg.DefaultConfig(),
			)
		},
	}

	sdkAppCreator := newSDKAppCreator()
	rootCommand.AddCommand(
		newInitCommand(defaultHome),
		newGenesisCommand(encodingConfig.TxConfig, defaultHome),
		cmtcli.NewCompletionCmd(rootCommand, true),
		evmdebug.Cmd(),
		confixcmd.ConfigCommand(),
		pruning.Cmd(sdkAppCreator, defaultHome),
		snapshot.Cmd(sdkAppCreator),
		cosmosevmcmd.KeyCommands(defaultHome, true),
		server.StatusCommand(),
		newQueryCommand(),
		newTxCommand(),
	)

	cosmosevmserver.AddCommands(
		rootCommand,
		cosmosevmserver.NewDefaultStartOptions(newApp, defaultHome),
		appExport,
		func(*cobra.Command) {},
	)
	if _, err := srvflags.AddTxFlags(rootCommand); err != nil {
		return nil, err
	}
	if err := enhanceAutoCLI(rootCommand, initialClientContext); err != nil {
		return nil, err
	}

	return rootCommand, nil
}

// setGRPCClientWithAppCodec replaces the generic protobuf connection created by the SDK
// flag reader. Cosmos SDK and Cosmos EVM query responses include gogoproto
// custom types such as LegacyDec, which require the application's gRPC codec.
// Clearing Changed prevents a module command's second flag-read pass from
// replacing this connection with the generic codec again.
func setGRPCClientWithAppCodec(
	command *cobra.Command,
	clientCtx client.Context,
	interfaceRegistry codectypes.InterfaceRegistry,
) (client.Context, error) {
	grpcFlag := command.Flags().Lookup(flags.FlagGRPC)
	if grpcFlag == nil || !grpcFlag.Changed {
		return clientCtx, nil
	}

	address, err := command.Flags().GetString(flags.FlagGRPC)
	if err != nil || address == "" {
		return clientCtx, err
	}
	useInsecure, err := command.Flags().GetBool(flags.FlagGRPCInsecure)
	if err != nil {
		return clientCtx, err
	}

	transportCredentials := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if useInsecure {
		transportCredentials = insecure.NewCredentials()
	}
	grpcClient, err := grpc.Dial(
		address,
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(sdkcodec.NewProtoCodec(interfaceRegistry).GRPCCodec()),
		),
	)
	if err != nil {
		return clientCtx, err
	}
	if clientCtx.GRPCClient != nil {
		_ = clientCtx.GRPCClient.Close()
	}
	grpcFlag.Changed = false
	return clientCtx.WithGRPCClient(grpcClient), nil
}

func newDefaultAppConfig() (string, any) {
	template, rawConfig := cosmosevmconfig.InitAppConfig(chainconfig.BaseDenom, chainconfig.EVMChainID)
	appConfig := rawConfig.(cosmosevmconfig.EVMAppConfig)
	appConfig.API.Enable = true
	appConfig.GRPC.Enable = true
	appConfig.JSONRPC.Enable = true
	appConfig.JSONRPC.AllowInsecureUnlock = false
	return template, appConfig
}
