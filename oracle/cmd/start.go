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

		cfgPath := configFilePath()
		cfg, err := config.LoadFile(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config from %s: %w", cfgPath, err)
		}

		if err := os.MkdirAll(homeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create home directory: %w", err)
		}

		log.Info().Str("home", homeDir()).Msg("starting oracled")

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		dmn, err := daemon.New(cfg)
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
