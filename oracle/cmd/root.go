package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	homeBase string
	rootCmd  = &cobra.Command{
		Use:   "oracled",
		Short: "Oracle Daemon for Guru Blockchain",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// CLI logs should be human-friendly text (not JSON).
			log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

			// Prevent "oracled init --home \"\"" from falling back to a relative ".oracled" directory.
			if strings.TrimSpace(homeBase) == "" {
				return fmt.Errorf("--home must not be empty")
			}
			return nil
		},
	}
)

func init() {
	userHome, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	rootCmd.PersistentFlags().StringVar(&homeBase, "home", userHome, "base directory for oracled (config will be under <home>/.oracled)")
}

func homeDir() string {
	return filepath.Join(homeBase, ".oracled")
}

func configFilePath() string {
	return filepath.Join(homeDir(), "config.toml")
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		log.Error().Err(err).Msg("failed to execute command")
		return err
	}
	return nil
}
