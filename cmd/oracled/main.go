package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	"github.com/gurufinglobal/guru/v3/oracle"
	"github.com/spf13/cobra"
)

const (
	defaultHomeDirName = ".oracled"

	flagConfig = "config"
	flagForce  = "force"
	flagHome   = "home"
	flagSocket = "socket"
)

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(rootCmd.ErrOrStderr(), err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	homeDir := mustDefaultHomeDir()

	rootCmd := &cobra.Command{
		Use:   "oracled",
		Short: "Guru oracle sidecar daemon",
	}
	rootCmd.PersistentFlags().String(flagHome, homeDir, "directory for oracle daemon configuration")

	rootCmd.AddCommand(
		startCommand(),
		initConfigCommand(),
	)

	return rootCmd
}

func startCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the oracle sidecar daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			homeDir, err := cmd.Root().PersistentFlags().GetString(flagHome)
			if err != nil {
				return err
			}
			configPath, err := cmd.Flags().GetString(flagConfig)
			if err != nil {
				return err
			}
			if configPath == "" {
				configPath = defaultConfigPath(homeDir)
			}

			cfg, err := oracle.LoadConfig(configPath)
			if err != nil {
				return err
			}
			socketOverride, err := cmd.Flags().GetString(flagSocket)
			if err != nil {
				return err
			}
			if socketOverride != "" {
				cfg.Socket = socketOverride
			}

			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			tasks, err := oracle.EnsureNodeTasksConfigured(runCtx, cfg)
			if err != nil {
				return err
			}

			sidecar, err := oracle.NewSidecar(cfg, tasks)
			if err != nil {
				return err
			}

			err = oracle.Start(runCtx, sidecar)
			if errors.Is(err, context.Canceled) {
				return nil
			}

			return err
		},
	}
	cmd.Flags().String(flagConfig, "", "path to oracle sidecar config file")
	cmd.Flags().String(flagSocket, "", "override Unix domain socket path")

	return cmd
}

func initConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-config",
		Short: "Write a default oracle sidecar config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			homeDir, err := cmd.Root().PersistentFlags().GetString(flagHome)
			if err != nil {
				return err
			}
			configPath, err := cmd.Flags().GetString(flagConfig)
			if err != nil {
				return err
			}
			if configPath == "" {
				configPath = defaultConfigPath(homeDir)
			}

			force, err := cmd.Flags().GetBool(flagForce)
			if err != nil {
				return err
			}
			if !force {
				if _, err := os.Stat(configPath); err == nil {
					return fmt.Errorf("config file already exists at %s", configPath)
				} else if !os.IsNotExist(err) {
					return err
				}
			}

			if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
				return err
			}

			return os.WriteFile(configPath, []byte(oracle.ConfigTemplate(homeDir)), 0o600)
		},
	}
	cmd.Flags().String(flagConfig, "", "path to oracle sidecar config file")
	cmd.Flags().Bool(flagForce, false, "overwrite an existing config file")

	return cmd
}

func defaultConfigPath(homeDir string) string {
	return filepath.Join(homeDir, "config", "oracled.toml")
}

func mustDefaultHomeDir() string {
	homeDir, err := clienthelpers.GetNodeHomeDirectory(defaultHomeDirName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting default oracle home directory:", err)
		os.Exit(1)
	}

	return homeDir
}
