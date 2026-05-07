package cmd

import (
	"os"

	"cosmossdk.io/log/v2"
	confixcmd "cosmossdk.io/tools/confix/cmd"
	cmtcli "github.com/cometbft/cometbft/libs/cli"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	clientcfg "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	cosmosevmcmd "github.com/cosmos/evm/client"
	evmdebug "github.com/cosmos/evm/client/debug"
	"github.com/cosmos/evm/crypto/hd"
	cosmosevmserver "github.com/cosmos/evm/server"
	srvflags "github.com/cosmos/evm/server/flags"
	"github.com/gurufinglobal/guru/v3/app"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	sdkCfg := sdk.GetConfig()
	sdkCfg.SetBech32PrefixForAccount(appparams.Bech32PrefixAccAddr, appparams.Bech32PrefixAccPub)
	sdkCfg.SetBech32PrefixForValidator(appparams.Bech32PrefixValAddr, appparams.Bech32PrefixValPub)
	sdkCfg.SetBech32PrefixForConsensusNode(appparams.Bech32PrefixConsAddr, appparams.Bech32PrefixConsPub)
	sdkCfg.Seal()

	nodeHome := appparams.MustDefaultHomeDir()

	tempApp := app.NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
	)

	initClientCtx := client.Context{}.
		WithCodec(tempApp.AppCodec()).
		WithInterfaceRegistry(tempApp.InterfaceRegistry()).
		WithTxConfig(tempApp.TxConfig()).
		WithLegacyAmino(nil).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode(flags.FlagBroadcastMode).
		WithHomeDir(nodeHome).
		WithViper(appparams.EnvName).
		WithKeyringOptions(hd.EthSecp256k1Option()).
		WithLedgerHasProtobuf(true).
		WithChainID(appparams.SDKChainID)

	rootCmd := &cobra.Command{
		Use:   appparams.EnvName,
		Short: "Guru Daemon",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) (err error) {
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			initClientCtx = initClientCtx.
				WithCmdContext(cmd.Context())

			if initClientCtx, err = client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags()); err != nil {
				return err
			}

			if initClientCtx, err = clientcfg.ReadFromClientConfig(initClientCtx); err != nil {
				return err
			}

			if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
				return err
			}

			appTemplate, appConfig := defaultAppToml()

			return sdkserver.InterceptConfigsPreRunHandler(cmd, appTemplate, appConfig, defaultConfigToml())
		},
	}

	sdkAppCreator := func(l log.Logger, d dbm.DB, ao servertypes.AppOptions) servertypes.Application {
		return newApp(l, d, ao)
	}

	rootCmd.AddCommand(
		genutilcli.InitCmd(tempApp.BasicModuleManager, nodeHome),
		genutilcli.Commands(tempApp.TxConfig(), tempApp.BasicModuleManager, nodeHome),
		cmtcli.NewCompletionCmd(rootCmd, true),
		evmdebug.Cmd(),
		confixcmd.ConfigCommand(),
		pruning.Cmd(sdkAppCreator, nodeHome),
		snapshot.Cmd(sdkAppCreator),
		cosmosevmcmd.KeyCommands(nodeHome, true),
		sdkserver.StatusCommand(),
		queryCommand(),
		txCommand(),
	)

	cosmosevmserver.AddCommands(
		rootCmd,
		cosmosevmserver.NewDefaultStartOptions(newApp, nodeHome),
		appExport,
		addModuleInitFlags,
	)

	if _, err := srvflags.AddTxFlags(rootCmd); err != nil {
		panic(err)
	}

	autoCliOpts := tempApp.AutoCliOpts()
	initClientCtx, _ = clientcfg.ReadFromClientConfig(initClientCtx)
	autoCliOpts.ClientCtx = initClientCtx

	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}

	return rootCmd
}
