package cmd

import (
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	clientcfg "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/evm/crypto/hd"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	encodingConfig := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)

	initClientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(nil).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode(flags.FlagBroadcastMode).
		WithHomeDir(appparams.MustDefaultHomeDir()).
		WithViper(appparams.EnvName).
		WithKeyringOptions(hd.EthSecp256k1Option()).
		WithLedgerHasProtobuf(true)

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

			return nil
		},
	}

	return nil
}

func defaultAppToml