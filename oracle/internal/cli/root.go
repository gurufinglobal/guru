package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
	"github.com/gurufinglobal/guru/oracle/internal/version"
	"github.com/spf13/cobra"
)

type commandError struct {
	exitCode int
	code     string
	err      error
	silent   bool
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

type execution struct {
	stdout  io.Writer
	stderr  io.Writer
	home    string
	command string
	format  string
}

func Run(args []string, stdout, stderr io.Writer) int {
	defaultHome, err := config.DefaultHome()
	if err != nil {
		_ = printHumanError(stderr, "", "internal", "", err)
		return 1
	}
	exec := &execution{stdout: stdout, stderr: stderr, home: defaultHome, format: "text"}
	exec.command, exec.format = inferCommandFormat(args)
	root := exec.rootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err = root.Execute()
	if err == nil {
		return 0
	}
	failure := &commandError{exitCode: 1, code: "internal", err: err}
	if !errors.As(err, &failure) {
		failure = &commandError{exitCode: 1, code: "invalid_arguments", err: err}
	}
	if failure.silent {
		return failure.exitCode
	}
	if exec.format == "json" && (exec.command == "status" || exec.command == "history" || exec.command == "reconcile") {
		envelope := service.ErrorEnvelope{
			SchemaVersion: 1,
			Command:       exec.command,
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
			Error: service.ErrorValue{
				Code:    failure.code,
				Message: boundedMessage(failure.err),
			},
		}
		encoded, encodeErr := json.Marshal(envelope)
		if encodeErr == nil {
			_, _ = fmt.Fprintf(stderr, "%s\n", encoded)
		}
	} else {
		_ = printHumanError(stderr, exec.command, failure.code, exec.home, failure.err)
	}
	return failure.exitCode
}

func (e *execution) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "oracled",
		Short: "Guru validator-local oracle sidecar",
		Long: "Guru validator-local oracle sidecar.\n\n" +
			"Start with 'oracled init', then run 'oracled start' in the foreground. " +
			"Use status, history, and reconcile from another terminal.",
		Version:       version.Version + " (" + version.Commit + ")",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&e.home, "home", e.home, "application home directory")
	root.PersistentFlags().Lookup("home").DefValue = printableASCII(e.home)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		e.initCommand(),
		e.validateCommand(),
		e.startCommand(),
		e.statusCommand(),
		e.historyCommand(),
		e.reconcileCommand(),
	)
	return root
}

func (e *execution) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a new sidecar home",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) (runErr error) {
			e.command = "init"
			absoluteHome, err := config.PrepareInitialHome(e.home)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			lockPath, err := config.CanonicalHomeLockPath(absoluteHome)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			lock, err := storage.AcquireHomeLock(lockPath)
			if err != nil {
				return fail(1, homeLockFailureCode(err), err)
			}
			defer func() {
				if closeErr := lock.Close(); closeErr != nil {
					runErr = errors.Join(runErr, fail(1, "storage_error", closeErr))
				}
			}()
			paths, err := config.WriteInitialFiles(absoluteHome)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			if err := storage.Initialize(paths.Database, paths.Marker); err != nil {
				return fail(1, "storage_error", err)
			}
			pair, err := config.Load(absoluteHome)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			if err := printInitialized(e.stdout, pair); err != nil {
				return fail(1, "internal", err)
			}
			return nil
		},
	}
}

func (e *execution) validateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the published configuration pair",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			e.command = "validate"
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			if err := printValidated(e.stdout, pair); err != nil {
				return fail(1, "internal", err)
			}
			return nil
		},
	}
}

func (e *execution) startCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run the sidecar in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			e.command = "start"
			lockPath, err := config.CanonicalHomeLockPath(e.home)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			lock, err := storage.AcquireHomeLock(lockPath)
			if err != nil {
				return fail(1, homeLockFailureCode(err), err)
			}
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(1, "invalid_config", errors.Join(err, lock.Close()))
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			guard := newShutdownGuard(pair.Config.Runtime.ShutdownTimeout.Duration, stop, os.Exit)
			defer func() {
				guard.Complete()
				stop()
			}()
			go func() {
				select {
				case <-ctx.Done():
					guard.Start()
				case <-guard.Done():
				}
			}()
			runErr := service.RunLocked(ctx, pair, lock, e.stdout, guard.Start)
			closeErr := lock.Close()
			guard.Complete()
			if err := errors.Join(runErr, closeErr); err != nil {
				return fail(1, startFailureCode(err), err)
			}
			if pair.Config.Logging.Format == "text" {
				if _, err := fmt.Fprintln(e.stdout, "Oracle daemon stopped."); err != nil {
					return fail(1, "internal", err)
				}
			}
			return nil
		},
	}
}

func (e *execution) statusCommand() *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "status [symbol]",
		Short: "Show daemon health or one configured feed",
		Long: "Show a live summary for every configured feed, or details for one symbol.\n\n" +
			"Friendly symbol forms such as btc-usd are accepted when they match exactly one configured feed. " +
			"In text mode, a stopped daemon is reported from lock-protected local storage.",
		Example: "  oracled status\n" +
			"  oracled status btc-usd\n" +
			"  oracled status --format json",
		Args: cobra.MaximumNArgs(1),
		PreRun: func(_ *cobra.Command, _ []string) {
			e.command, e.format = "status", format
		},
		RunE: func(command *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fail(1, "invalid_arguments", errors.New("--format must be text or json"))
			}
			if len(args) == 1 && format == "json" {
				return fail(1, "invalid_arguments", errors.New("JSON status does not accept a symbol"))
			}
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			rawSymbol := ""
			symbol := ""
			if len(args) == 1 {
				rawSymbol = args[0]
				symbol, err = resolveConfiguredSymbol(rawSymbol, pair.Feeds)
				if err != nil {
					return fail(1, "invalid_arguments", err)
				}
			}
			body, statusCode, err := fetchAdmin(command.Context(), pair.Paths.AdminSocket, "/v1/status")
			if err != nil {
				if format == "text" && isAdminTransportError(err) {
					view, selected, offlineErr := offlineStatus(e.home, rawSymbol)
					if offlineErr != nil {
						return commandFailureForOffline(1, offlineErr)
					}
					if err := printOfflineStatus(e.stdout, view, selected, time.Now()); err != nil {
						return fail(1, "internal", err)
					}
					return nil
				}
				return fail(1, adminFailureCode(err), err)
			}
			if statusCode != http.StatusOK {
				return fail(1, "protocol_error", decodeAdminError(body))
			}
			envelope, err := decodeSuccess[service.StatusData](body, "status")
			if err != nil {
				return fail(1, "protocol_error", err)
			}
			if format == "json" {
				return writeRawJSON(e.stdout, body)
			}
			if err := validateHumanStatusData(envelope.Data); err != nil {
				return fail(1, "protocol_error", err)
			}
			selected, err := selectLiveFeed(envelope.Data, symbol)
			if err != nil {
				return fail(1, "protocol_error", err)
			}
			if err := printLiveStatus(e.stdout, pair.Paths.Home, envelope.Data, selected, time.Now()); err != nil {
				return fail(1, "internal", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", "text", "output format: text or json")
	return command
}

func (e *execution) historyCommand() *cobra.Command {
	var (
		format   string
		pageSize uint32
		pageKey  string
		offline  bool
	)
	command := &cobra.Command{
		Use:   "history [symbol]",
		Short: "Show stored aggregates or one symbol's records",
		Long: "Show the latest stored aggregate for every configured feed, or bounded history for one symbol.\n\n" +
			"Friendly symbol forms such as btc-usd are accepted when unique. Text mode automatically uses " +
			"lock-protected local storage when the daemon is stopped.",
		Example: "  oracled history\n" +
			"  oracled history btc-usd\n" +
			"  oracled history BTC/USD --page-size 10",
		Args: cobra.MaximumNArgs(1),
		PreRun: func(_ *cobra.Command, _ []string) {
			e.command, e.format = "history", format
		},
		RunE: func(command *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fail(1, "invalid_arguments", errors.New("--format must be text or json"))
			}
			if pageSize < storage.MinHistoryPageSize || pageSize > storage.MaxHistoryPageSize {
				return fail(1, "invalid_arguments", errors.New("--page-size must be from 1 to 50"))
			}
			if len(args) == 0 {
				if format == "json" {
					return fail(1, "invalid_arguments", errors.New("JSON history requires a symbol"))
				}
				if command.Flags().Changed("page-size") || command.Flags().Changed("page-key") {
					return fail(1, "invalid_arguments", errors.New("history summary does not accept pagination flags"))
				}
				if offline {
					view, err := offlineHistorySummary(e.home)
					if err != nil {
						return commandFailureForOffline(1, err)
					}
					if err := printHistorySummary(e.stdout, view, time.Now()); err != nil {
						return fail(1, "internal", err)
					}
					return nil
				}
				pair, err := config.Load(e.home)
				if err != nil {
					return fail(1, "invalid_config", err)
				}
				view, err := liveHistorySummary(command.Context(), pair)
				if err != nil {
					if isAdminTransportError(err) {
						view, offlineErr := offlineHistorySummary(e.home)
						if offlineErr != nil {
							return commandFailureForOffline(1, offlineErr)
						}
						if err := printHistorySummary(e.stdout, view, time.Now()); err != nil {
							return fail(1, "internal", err)
						}
						return nil
					}
					return fail(1, adminFailureCode(err), err)
				}
				if err := printHistorySummary(e.stdout, view, time.Now()); err != nil {
					return fail(1, "internal", err)
				}
				return nil
			}
			if err := ensureHistoryPageKey(pageKey); err != nil {
				return fail(1, "invalid_arguments", errors.New("--page-key is invalid"))
			}
			if offline {
				data, resolvedHome, err := offlineHistoryDetail(
					e.home,
					args[0],
					pageSize,
					pageKey,
					format == "text",
				)
				if err != nil {
					return commandFailureForOffline(1, err)
				}
				if format == "json" {
					envelope := service.SuccessEnvelope[service.HistoryData]{
						SchemaVersion: 1,
						Command:       "history",
						GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
						Data:          data,
					}
					encoded, err := json.Marshal(envelope)
					if err != nil {
						return fail(1, "internal", err)
					}
					return writeRawJSON(e.stdout, encoded)
				}
				if err := printHistoryDetail(e.stdout, resolvedHome, "offline", data, pageSize, true, time.Now()); err != nil {
					return fail(1, "internal", err)
				}
				return nil
			}
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			symbol, err := resolveConfiguredSymbol(args[0], pair.Feeds)
			if err != nil {
				return fail(1, "invalid_arguments", err)
			}
			body, data, err := fetchHistory(
				command.Context(),
				pair.Paths.AdminSocket,
				symbol,
				pageSize,
				pageKey,
				format == "text",
			)
			if err != nil {
				if format == "text" && isAdminTransportError(err) {
					offlineData, resolvedHome, offlineErr := offlineHistoryDetail(
						e.home,
						args[0],
						pageSize,
						pageKey,
						true,
					)
					if offlineErr != nil {
						return commandFailureForOffline(1, offlineErr)
					}
					if err := printHistoryDetail(
						e.stdout,
						resolvedHome,
						"offline",
						offlineData,
						pageSize,
						true,
						time.Now(),
					); err != nil {
						return fail(1, "internal", err)
					}
					return nil
				}
				return fail(1, adminFailureCode(err), err)
			}
			if format == "json" {
				return writeRawJSON(e.stdout, body)
			}
			if err := printHistoryDetail(e.stdout, pair.Paths.Home, "live", data, pageSize, false, time.Now()); err != nil {
				return fail(1, "internal", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", "text", "output format: text or json")
	command.Flags().Uint32Var(&pageSize, "page-size", storage.DefaultHistoryPageSize, "history records per page (1-50)")
	command.Flags().StringVar(&pageKey, "page-key", "", "opaque continuation key")
	command.Flags().BoolVar(&offline, "offline", false, "read storage directly while the daemon is stopped")
	return command
}

func (e *execution) reconcileCommand() *cobra.Command {
	var (
		format   string
		nodeGRPC string
	)
	command := &cobra.Command{
		Use:   "reconcile",
		Short: "Compare live sidecar readiness with one node",
		Long: "Compare the running sidecar with active Oracle tasks from a Guru node.\n\n" +
			"The command is read-only. It reports whether this validator is ready to contribute and lists " +
			"operator actions for any mismatch.",
		Example: "  oracled reconcile\n" +
			"  oracled reconcile --node-grpc 127.0.0.1:9090\n" +
			"  oracled reconcile --format json",
		Args: cobra.NoArgs,
		PreRun: func(_ *cobra.Command, _ []string) {
			e.command, e.format = "reconcile", format
		},
		RunE: func(command *cobra.Command, _ []string) error {
			if format != "text" && format != "json" {
				return fail(2, "invalid_arguments", errors.New("--format must be text or json"))
			}
			if strings.TrimSpace(nodeGRPC) == "" {
				return fail(2, "invalid_arguments", errors.New("--node-grpc must not be empty"))
			}
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(2, "invalid_config", err)
			}
			body, statusCode, err := fetchAdmin(command.Context(), pair.Paths.AdminSocket, "/v1/status")
			if err != nil {
				return fail(2, adminFailureCode(err), err)
			}
			if statusCode != http.StatusOK {
				return fail(2, "protocol_error", decodeAdminError(body))
			}
			statusEnvelope, err := decodeSuccess[service.StatusData](body, "status")
			if err != nil {
				return fail(2, "protocol_error", err)
			}
			if format == "text" {
				if err := validateHumanStatusData(statusEnvelope.Data); err != nil {
					return fail(2, "protocol_error", err)
				}
			}
			data, err := reconcile(
				command.Context(),
				nodeGRPC,
				statusEnvelope.Data,
				pair.Config.PublicationRevision,
				pair.Config.SourcesSHA256,
			)
			if err != nil {
				code := "node_unavailable"
				if isReconcileProtocolError(err) {
					code = "protocol_error"
				}
				return fail(2, code, err)
			}
			envelope := service.SuccessEnvelope[ReconcileData]{
				SchemaVersion: 1,
				Command:       "reconcile",
				GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
				Data:          data,
			}
			if format == "json" {
				encoded, err := json.Marshal(envelope)
				if err != nil {
					return fail(2, "internal", err)
				}
				if err := writeRawJSON(e.stdout, encoded); err != nil {
					return fail(2, "internal", err)
				}
			} else {
				if err := printReconcile(e.stdout, statusEnvelope.Data, data); err != nil {
					return fail(2, "internal", err)
				}
			}
			for _, finding := range data.Findings {
				if finding.Blocking {
					return &commandError{exitCode: 1, silent: true, err: errors.New("readiness mismatch")}
				}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", "text", "output format: text or json")
	command.Flags().StringVar(&nodeGRPC, "node-grpc", "127.0.0.1:9090", "Guru node gRPC endpoint")
	return command
}

func fail(exit int, code string, err error) error {
	return &commandError{exitCode: exit, code: code, err: err}
}

func startFailureCode(err error) string {
	if errors.Is(err, storage.ErrHomeLocked) {
		return "home_locked"
	}
	if errors.Is(err, service.ErrStorageUnavailable) {
		return "storage_error"
	}
	return "internal"
}

func homeLockFailureCode(err error) string {
	if errors.Is(err, storage.ErrHomeLocked) {
		return "home_locked"
	}
	return "storage_error"
}

func adminFailureCode(err error) string {
	if isAdminProtocolError(err) {
		return "protocol_error"
	}
	return "daemon_unavailable"
}

func inferCommandFormat(args []string) (string, string) {
	format := "text"
	command := ""
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--format" && i+1 < len(args):
			format = args[i+1]
			i++
		case strings.HasPrefix(argument, "--format="):
			format = strings.TrimPrefix(argument, "--format=")
		case argument == "--home" && i+1 < len(args):
			i++
		case strings.HasPrefix(argument, "--home="):
		case argument == "status" || argument == "history" || argument == "reconcile":
			command = argument
		}
	}
	return command, format
}
