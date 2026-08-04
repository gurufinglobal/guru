package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
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
		_, _ = fmt.Fprintf(stderr, "Error: %s\n", boundedMessage(err))
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
		_, _ = fmt.Fprintf(stderr, "Error: %s\n", boundedMessage(failure.err))
	}
	return failure.exitCode
}

func (e *execution) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "oracled",
		Short:         "Guru validator-local oracle sidecar",
		Version:       version.Version + " (" + version.Commit + ")",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&e.home, "home", e.home, "application home directory")
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
			if _, err := config.Load(absoluteHome); err != nil {
				return fail(1, "invalid_config", err)
			}
			_, err = fmt.Fprintf(e.stdout, "Initialized oracled home at %s\n", paths.Home)
			if err != nil {
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
			if _, err := config.Load(e.home); err != nil {
				return fail(1, "invalid_config", err)
			}
			if _, err := fmt.Fprintln(e.stdout, "Configuration is valid."); err != nil {
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
			return nil
		},
	}
}

func (e *execution) statusCommand() *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "status",
		Short: "Inspect live sidecar status",
		Args:  cobra.NoArgs,
		PreRun: func(_ *cobra.Command, _ []string) {
			e.command, e.format = "status", format
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if format != "text" && format != "json" {
				return fail(1, "invalid_arguments", errors.New("--format must be text or json"))
			}
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(1, "invalid_config", err)
			}
			body, statusCode, err := fetchAdmin(context.Background(), pair.Paths.AdminSocket, "/v1/status")
			if err != nil {
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
			if err := printStatus(e.stdout, envelope.Data); err != nil {
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
		Use:   "history <symbol>",
		Short: "Inspect bounded aggregate history",
		Args:  cobra.ExactArgs(1),
		PreRun: func(_ *cobra.Command, _ []string) {
			e.command, e.format = "history", format
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fail(1, "invalid_arguments", errors.New("--format must be text or json"))
			}
			if pageSize < storage.MinHistoryPageSize || pageSize > storage.MaxHistoryPageSize {
				return fail(1, "invalid_arguments", errors.New("--page-size must be from 1 to 50"))
			}
			symbol, err := domain.NormalizeSymbol(args[0])
			if err != nil || symbol != args[0] {
				return fail(1, "invalid_arguments", errors.New("history symbol must be canonical"))
			}
			if _, err := storage.DecodePageKey(pageKey); err != nil {
				return fail(1, "invalid_arguments", errors.New("--page-key is invalid"))
			}
			var body []byte
			if offline {
				lockPath, lockErr := config.CanonicalHomeLockPath(e.home)
				if lockErr != nil {
					return fail(1, "invalid_config", lockErr)
				}
				lock, lockErr := storage.AcquireHomeLock(lockPath)
				if lockErr != nil {
					return fail(1, homeLockFailureCode(lockErr), lockErr)
				}
				pair, loadErr := config.Load(e.home)
				if loadErr != nil {
					return fail(1, "invalid_config", errors.Join(loadErr, lock.Close()))
				}
				body, err = offlineHistory(pair, symbol, pageSize, pageKey)
				if err != nil {
					return fail(1, "storage_error", errors.Join(err, lock.Close()))
				}
				if err := lock.Close(); err != nil {
					return fail(1, "storage_error", err)
				}
			} else {
				pair, loadErr := config.Load(e.home)
				if loadErr != nil {
					return fail(1, "invalid_config", loadErr)
				}
				query := url.Values{}
				query.Set("symbol", symbol)
				query.Set("page_size", fmt.Sprintf("%d", pageSize))
				if pageKey != "" {
					query.Set("page_key", pageKey)
				}
				statusCode := 0
				body, statusCode, err = fetchAdmin(context.Background(), pair.Paths.AdminSocket, "/v1/history?"+query.Encode())
				if err != nil {
					return fail(1, adminFailureCode(err), err)
				}
				if statusCode != http.StatusOK {
					return fail(1, "protocol_error", decodeAdminError(body))
				}
			}
			envelope, err := decodeSuccess[service.HistoryData](body, "history")
			if err != nil {
				return fail(1, "protocol_error", err)
			}
			if format == "json" {
				return writeRawJSON(e.stdout, body)
			}
			if err := printHistory(e.stdout, envelope.Data); err != nil {
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
		Args:  cobra.NoArgs,
		PreRun: func(_ *cobra.Command, _ []string) {
			e.command, e.format = "reconcile", format
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if format != "text" && format != "json" {
				return fail(2, "invalid_arguments", errors.New("--format must be text or json"))
			}
			if strings.TrimSpace(nodeGRPC) == "" {
				return fail(2, "invalid_arguments", errors.New("--node-grpc is required"))
			}
			pair, err := config.Load(e.home)
			if err != nil {
				return fail(2, "invalid_config", err)
			}
			body, statusCode, err := fetchAdmin(context.Background(), pair.Paths.AdminSocket, "/v1/status")
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
			data, err := reconcile(
				context.Background(),
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
				if err := printReconcile(e.stdout, data); err != nil {
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
	command.Flags().StringVar(&nodeGRPC, "node-grpc", "", "Guru node gRPC endpoint")
	return command
}

func offlineHistory(pair *config.Pair, symbol string, pageSize uint32, pageKey string) (body []byte, err error) {
	store, err := storage.Open(pair.Paths.Database, pair.Paths.Marker, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, store.Close())
	}()
	token, err := storage.DecodePageKey(pageKey)
	if err != nil {
		return nil, err
	}
	page, err := store.History(symbol, pageSize, token)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	envelope := service.SuccessEnvelope[service.HistoryData]{
		SchemaVersion: 1,
		Command:       "history",
		GeneratedAt:   now.UTC().Format(time.RFC3339Nano),
		Data:          service.BuildHistoryData(page, store.Catalog()),
	}
	return json.Marshal(envelope)
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

func printStatus(output io.Writer, data service.StatusData) error {
	if _, err := fmt.Fprintf(output, "Health: %s\nGeneration: %s\n", data.Health, data.ActivationGeneration); err != nil {
		return err
	}
	for _, feed := range data.Feeds {
		if _, err := fmt.Fprintf(output, "%s  %s  %s  sources=%d\n", feed.Symbol, feed.Health, feed.Freshness, feed.ConfiguredSourceCount); err != nil {
			return err
		}
	}
	return nil
}

func printHistory(output io.Writer, data service.HistoryData) error {
	if _, err := fmt.Fprintf(output, "History for %s (high water %s)\n", data.Symbol, data.HighWaterSequence); err != nil {
		return err
	}
	for _, record := range data.Records {
		if _, err := fmt.Fprintf(output, "%s  %s  sources=%d  %s\n", record.Sequence, record.Value, record.SuccessfulSourceCount, record.CollectedAt); err != nil {
			return err
		}
	}
	if data.NextPageKey != nil {
		if _, err := fmt.Fprintf(output, "Next page key: %s\n", *data.NextPageKey); err != nil {
			return err
		}
	}
	return nil
}

func printReconcile(output io.Writer, data ReconcileData) error {
	if _, err := fmt.Fprintf(output, "Node: %s\nActive tasks: %d\nMinimum sources: %d\n", data.NodeGRPC, data.ActiveTaskCount, data.MinSources); err != nil {
		return err
	}
	if len(data.Findings) == 0 {
		_, err := fmt.Fprintln(output, "Ready: no mismatches found.")
		return err
	}
	findings := append([]Finding(nil), data.Findings...)
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Code < findings[j].Code })
	for _, finding := range findings {
		severity := "info"
		if finding.Blocking {
			severity = "blocking"
		}
		symbol := "-"
		if finding.Symbol != nil {
			symbol = *finding.Symbol
		}
		if _, err := fmt.Fprintf(output, "%s  %s  %s  %s\n", severity, finding.Code, symbol, finding.Message); err != nil {
			return err
		}
	}
	return nil
}
