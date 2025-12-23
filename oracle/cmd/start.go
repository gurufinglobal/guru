package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/gurufinglobal/guru/v2/oracle/config"
	"github.com/gurufinglobal/guru/v2/oracle/daemon"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the oracle daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		// human-friendly console logging
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

		// Require init first: do not create homeDir implicitly on start.
		if st, err := os.Stat(homeDir()); err != nil || !st.IsDir() {
			return fmt.Errorf("home directory not initialized at %s (run `oracled init` first)", homeDir())
		}

		cfgPath := configFilePath()
		cfg, err := config.LoadFile(cfgPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("config not found at %s (run `oracled init` first)", cfgPath)
			}
			return fmt.Errorf("failed to load config from %s: %w", cfgPath, err)
		}

		// If keyring backend uses a filesystem directory, ensure it exists before starting.
		// Example: backend "test" => "<home>/.oracled/keyring-test".
		if cfg.Keyring.Backend == "test" || cfg.Keyring.Backend == "file" {
			krDir := filepath.Join(homeDir(), "keyring-"+cfg.Keyring.Backend)
			if st, err := os.Stat(krDir); err != nil || !st.IsDir() {
				return fmt.Errorf("keyring directory not found at %s (backend=%s); add key first, then run `oracled start` again", krDir, cfg.Keyring.Backend)
			}
		}

		log.Info().Str("home", homeDir()).Msg("starting oracled")

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		dmn, err := daemon.New(cfg, homeDir())
		if err != nil {
			return fmt.Errorf("failed to create daemon: %w", err)
		}

		if err := dmn.Start(ctx); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}

		<-ctx.Done()
		log.Info().Msg("shutting down")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
