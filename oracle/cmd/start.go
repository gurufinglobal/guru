package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
			log.Error().Str("home", homeDir()).Msg("home directory not initialized; run `oracled init` first")
			return nil
		}

		cfgPath := configFilePath()
		cfg, err := config.LoadFile(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config from %s (run `oracled init` first): %w", cfgPath, err)
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
