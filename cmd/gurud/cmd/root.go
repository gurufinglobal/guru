// Package cmd defines the minimal gurud command surface for Stage A.
package cmd

import (
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v2/app"
	"github.com/gurufinglobal/guru/v2/config"
)

// NewRootCmd creates a version-only command. In particular, it does not
// register server, start, init, query, or transaction commands.
func NewRootCmd() (*cobra.Command, error) {
	if err := config.SetupSDKConfig(); err != nil {
		return nil, err
	}

	encodingConfig, err := app.MakeEncodingConfig()
	if err != nil {
		return nil, err
	}
	home, err := config.DefaultNodeHome()
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
		WithHomeDir(home).
		WithViper(config.EnvPrefix)

	rootCommand := &cobra.Command{
		Use:           config.BinaryName,
		Short:         "Guru application skeleton",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          client.ValidateCmd,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			commandContext := initialClientContext.
				WithCmdContext(command.Context()).
				WithInput(command.InOrStdin()).
				WithOutput(command.OutOrStdout())
			return client.SetCmdClientContextHandler(commandContext, command)
		},
	}

	rootCommand.AddCommand(sdkversion.NewVersionCommand())
	return rootCommand, nil
}
