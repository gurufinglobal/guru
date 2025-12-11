package cmd

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v2/oracle/config"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(homeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create home directory: %w", err)
		}

		cfgPath := configFilePath()
		if _, err := os.Stat(cfgPath); err == nil {
			return fmt.Errorf("config already exists at %s", cfgPath)
		}

		if err := config.WriteDefaultFile(cfgPath); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}

		log.Info().Str("path", cfgPath).Msg("initialized oracled config")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
