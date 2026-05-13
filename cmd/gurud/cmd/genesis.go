package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/gurufinglobal/guru/v3/app"
)

const chainUpgradeGuide = "https://github.com/cosmos/cosmos-sdk/blob/main/UPGRADING.md"

func genesisCommand(tempApp *app.App, defaultNodeHome string) *cobra.Command {
	genesisCmd := genutilcli.Commands(tempApp.TxConfig(), tempApp.BasicModuleManager, defaultNodeHome)

	for _, sub := range genesisCmd.Commands() {
		if sub.Name() == "validate" {
			genesisCmd.RemoveCommand(sub)
			break
		}
	}

	genesisCmd.AddCommand(ValidateGenesisCmd(tempApp))

	return genesisCmd
}

// ValidateGenesisCmd validates the genesis with both module-level and chain-level rules.
func ValidateGenesisCmd(tempApp *app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "validate [file]",
		Aliases: []string{"validate-genesis"},
		Args:    cobra.RangeArgs(0, 1),
		Short:   "Validates the genesis file at the default location or at the location passed as an arg",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			genesisFile := serverCtx.Config.GenesisFile()
			if len(args) == 1 {
				genesisFile = args[0]
			}

			appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
			if err != nil {
				return enrichGenesisUnmarshalError(err)
			}

			if err := appGenesis.ValidateAndComplete(); err != nil {
				return fmt.Errorf(
					"make sure that you have correctly migrated all CometBFT consensus params. Refer the UPGRADING.md (%s): %w",
					chainUpgradeGuide,
					err,
				)
			}

			var genState map[string]json.RawMessage
			if err := json.Unmarshal(appGenesis.AppState, &genState); err != nil {
				if strings.Contains(err.Error(), "unexpected end of JSON input") {
					return fmt.Errorf("app_state is missing in the genesis file: %s", err.Error())
				}
				return fmt.Errorf("error unmarshalling genesis doc %s: %w", genesisFile, err)
			}

			if err := tempApp.ValidateChainGenesis(genState); err != nil {
				errStr := fmt.Sprintf("error validating chain genesis file %s: %s", genesisFile, err.Error())
				if errors.Is(err, io.EOF) {
					errStr = fmt.Sprintf("%s: section is missing in the app_state", errStr)
				}
				return fmt.Errorf("%s", errStr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "File at %s is a valid chain genesis file\n", genesisFile)
			return nil
		},
	}
}

func enrichGenesisUnmarshalError(err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("error at offset %d: %s", syntaxErr.Offset, syntaxErr.Error())
	}
	return err
}
