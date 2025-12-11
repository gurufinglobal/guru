package cmd

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	homeBase string
	rootCmd  = &cobra.Command{
		Use:   "oracled",
		Short: "Oracle Daemon for Guru Blockchain",
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
