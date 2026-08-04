package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/collector"
	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
	oraclev1 "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	consumerMaxConnections       = 16
	consumerMaxConcurrentStreams = 16
	consumerMaxHeaderListBytes   = 16 << 10
	consumerMaxConnectionIdle    = 30 * time.Second
	maxInFlightSourceBodyBytes   = 32 << 20
)

var (
	ErrStorageUnavailable = errors.New("sidecar storage is unavailable")
	ErrShutdownIncomplete = errors.New("sidecar shutdown deadline exceeded")
)

// RunLocked runs with ownership of an already acquired canonical home lock.
// The caller must acquire the lock before loading the configuration pair.
func RunLocked(
	ctx context.Context,
	pair *config.Pair,
	lock *storage.HomeLock,
	output io.Writer,
	shutdownStarted func(),
) (runErr error) {
	if pair == nil {
		return errors.New("configuration pair is nil")
	}
	if lock == nil {
		return errors.New("canonical home lock is not held")
	}

	store, err := storage.Open(pair.Paths.Database, pair.Paths.Marker, false)
	if err != nil {
		return storageRunError("open storage", err)
	}
	closeStoreOnReturn := true
	defer func() {
		if closeStoreOnReturn {
			runErr = errors.Join(runErr, storageRunError("close storage", store.Close()))
		}
	}()
	catalog, err := store.Activate(
		pair.PlanDigest,
		pair.Feeds,
		pair.Config.Storage.HistoryRetention,
		true,
	)
	if err != nil {
		return storageRunError("activate feed plan", err)
	}
	latest, err := store.LatestRecords()
	if err != nil {
		return storageRunError("recover latest aggregates", err)
	}
	diagnostics := newRuntimeDiagnostics(output, pair.Config.Logging, pair.Paths.Home)
	diagnosticsClosed := false
	defer func() {
		if diagnosticsClosed {
			return
		}
		closeContext, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		runErr = errors.Join(runErr, diagnostics.Close(closeContext))
	}()
	state := newState(pair, catalog, latest, time.Now(), diagnostics)
	sourceClient, err := collector.NewSourceClient(pair.CollectorPolicy)
	if err != nil {
		return fmt.Errorf("initialize source client: %w", err)
	}
	defer sourceClient.CloseIdleConnections()

	listener, err := listenPrivateUnix(pair.Paths.ConsumerSocket)
	if err != nil {
		return fmt.Errorf("bind consumer socket: %w", err)
	}
	consumerListener := newCloseOnceListener(listener)
	consumerIdentity, err := os.Lstat(pair.Paths.ConsumerSocket)
	if err != nil {
		return errors.Join(
			fmt.Errorf("inspect consumer socket: %w", err),
			ignoreListenerClosed(consumerListener.Close()),
		)
	}
	defer func() {
		runErr = errors.Join(
			runErr,
			ignoreListenerClosed(consumerListener.Close()),
			removeOwnedSocket(pair.Paths.ConsumerSocket, consumerIdentity),
		)
	}()
	listener, err = listenPrivateUnix(pair.Paths.AdminSocket)
	if err != nil {
		return fmt.Errorf("bind admin socket: %w", err)
	}
	adminListener := newCloseOnceListener(listener)
	adminIdentity, err := os.Lstat(pair.Paths.AdminSocket)
	if err != nil {
		return errors.Join(
			fmt.Errorf("inspect admin socket: %w", err),
			ignoreListenerClosed(adminListener.Close()),
		)
	}
	defer func() {
		runErr = errors.Join(
			runErr,
			ignoreListenerClosed(adminListener.Close()),
			removeOwnedSocket(pair.Paths.AdminSocket, adminIdentity),
		)
	}()

	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(int(pair.Config.Server.MaxRequestBytes)),
		grpc.MaxSendMsgSize(int(pair.Config.Server.MaxResponseBytes)),
		grpc.MaxConcurrentStreams(consumerMaxConcurrentStreams),
		grpc.MaxHeaderListSize(consumerMaxHeaderListBytes),
		grpc.ConnectionTimeout(2*time.Second),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: consumerMaxConnectionIdle,
		}),
	)
	oraclev1.RegisterOracleSidecarServer(grpcServer, NewConsumerServer(
		state,
		pair.Config.Server.MaxRequestBytes,
		pair.Config.Server.MaxResponseBytes,
	))
	fatal := make(chan error, 3)
	adminHandler := newTrackedAdminHandler(newAdminServerWithFatal(state, store, func(err error) {
		select {
		case fatal <- storageRunError("serve admin history", err):
		default:
		}
	}))
	adminServer := &http.Server{
		Handler:           adminHandler,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	engine := collector.NewEngine(
		pair.Feeds,
		catalog.ActivationGeneration,
		effectiveCollectorConcurrency(pair.CollectorPolicy),
		sourceClient,
		func(candidate domain.Aggregate) (domain.Aggregate, error) {
			record, insertErr := store.Insert(candidate, pair.Config.Storage.HistoryRetention)
			return record, storageRunError("insert aggregate", insertErr)
		},
		state,
	)

	var stopping atomic.Bool
	var servers sync.WaitGroup
	servers.Add(2)
	go func() {
		defer servers.Done()
		if serveErr := grpcServer.Serve(limitListener(consumerListener, consumerMaxConnections)); serveErr != nil &&
			!stopping.Load() {
			select {
			case fatal <- fmt.Errorf("consumer listener stopped: %w", serveErr):
			default:
			}
		}
	}()
	serversDone := make(chan struct{})
	go func() {
		servers.Wait()
		close(serversDone)
	}()
	go func() {
		defer servers.Done()
		if serveErr := adminServer.Serve(limitListener(adminListener, 16)); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) && !stopping.Load() {
			select {
			case fatal <- fmt.Errorf("admin listener stopped: %w", serveErr):
			default:
			}
		}
	}()
	diagnostics.Ready(uint32(len(pair.Feeds)))
	engineDone := make(chan error, 1)
	go func() { engineDone <- engine.Run(runContext) }()
	var engineStopped bool
	select {
	case <-ctx.Done():
	case runErr = <-fatal:
	case runErr = <-engineDone:
		engineStopped = true
		if runErr == nil && ctx.Err() == nil {
			runErr = errors.New("collector stopped unexpectedly")
		}
	}

	if shutdownStarted != nil {
		shutdownStarted()
	}
	stopping.Store(true)
	adminHandler.Stop()
	cancel()
	sourceClient.CloseIdleConnections()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), pair.Config.Runtime.ShutdownTimeout.Duration)
	defer shutdownCancel()
	adminDone := make(chan error, 1)
	go func() {
		adminDone <- adminServer.Shutdown(shutdownContext)
	}()
	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()
	var engineWait <-chan error
	if !engineStopped {
		engineWait = engineDone
	}
	adminWait := (<-chan error)(adminDone)
	grpcWait := (<-chan struct{})(grpcDone)
	deadline := shutdownContext.Done()
	serverWait := (<-chan struct{})(serversDone)
	shutdownTimedOut := false
shutdownLoop:
	for adminWait != nil || grpcWait != nil || engineWait != nil || serverWait != nil {
		select {
		case adminErr := <-adminWait:
			adminWait = nil
			if adminErr != nil &&
				!errors.Is(adminErr, http.ErrServerClosed) &&
				!errors.Is(adminErr, context.Canceled) &&
				!errors.Is(adminErr, context.DeadlineExceeded) {
				runErr = errors.Join(runErr, adminErr)
			}
		case <-grpcWait:
			grpcWait = nil
		case engineErr := <-engineWait:
			engineWait = nil
			if engineErr != nil {
				runErr = errors.Join(runErr, engineErr)
			}
		case <-serverWait:
			serverWait = nil
		case <-deadline:
			shutdownTimedOut = true
			runErr = errors.Join(runErr, ErrShutdownIncomplete)
			runErr = errors.Join(runErr, ignoreServerClosed(adminServer.Close()))
			grpcServer.Stop()
			break shutdownLoop
		}
	}
	if engineWait != nil {
		select {
		case engineErr := <-engineWait:
			engineWait = nil
			if engineErr != nil {
				runErr = errors.Join(runErr, engineErr)
			}
		default:
		}
	}
	handlersDrained := true
	if drainErr := adminHandler.Wait(shutdownContext); drainErr != nil {
		handlersDrained = false
		runErr = errors.Join(runErr, fmt.Errorf("drain admin handlers: %w", drainErr))
		if !shutdownTimedOut {
			runErr = errors.Join(runErr, ignoreServerClosed(adminServer.Close()))
		}
	}
	if diagnosticsErr := diagnostics.Close(shutdownContext); diagnosticsErr != nil {
		runErr = errors.Join(runErr, diagnosticsErr)
	}
	diagnosticsClosed = true
	if engineWait != nil || !handlersDrained {
		// A timed-out process may still have a collector or admin request using
		// bbolt. Leave it open for process teardown instead of racing that work.
		closeStoreOnReturn = false
		if !errors.Is(runErr, ErrShutdownIncomplete) {
			runErr = errors.Join(runErr, ErrShutdownIncomplete)
		}
		return runErr
	}
	runErr = errors.Join(runErr, storageRunError("sync storage", store.Sync()))
	closeErr := store.Close()
	closeStoreOnReturn = false
	runErr = errors.Join(runErr, storageRunError("close storage", closeErr))
	return runErr
}

func storageRunError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s: %w", ErrStorageUnavailable, operation, err)
}

func effectiveCollectorConcurrency(policy domain.CollectorPolicy) uint32 {
	bodyLimited := uint32(maxInFlightSourceBodyBytes / uint64(policy.SourceResponseBytes))
	if bodyLimited < 1 {
		bodyLimited = 1
	}
	return min(policy.MaxConcurrency, bodyLimited)
}

type trackedAdminHandler struct {
	next http.Handler

	mu        sync.Mutex
	active    int
	stopped   bool
	drained   chan struct{}
	drainOnce sync.Once
}

func newTrackedAdminHandler(next http.Handler) *trackedAdminHandler {
	return &trackedAdminHandler{next: next, drained: make(chan struct{})}
}

func (h *trackedAdminHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		http.Error(writer, "server is shutting down", http.StatusServiceUnavailable)
		return
	}
	h.active++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.active--
		drained := h.stopped && h.active == 0
		h.mu.Unlock()
		if drained {
			h.drainOnce.Do(func() { close(h.drained) })
		}
	}()
	h.next.ServeHTTP(writer, request)
}

func (h *trackedAdminHandler) Stop() {
	h.mu.Lock()
	h.stopped = true
	drained := h.active == 0
	h.mu.Unlock()
	if drained {
		h.drainOnce.Do(func() { close(h.drained) })
	}
}

func (h *trackedAdminHandler) Wait(ctx context.Context) error {
	select {
	case <-h.drained:
		return nil
	default:
	}
	select {
	case <-h.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ignoreListenerClosed(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func ignoreServerClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func removeOwnedSocket(path string, identity os.FileInfo) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect owned socket: %w", err)
	}
	if identity == nil ||
		!os.SameFile(identity, info) ||
		info.Mode()&os.ModeSocket == 0 ||
		info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove owned socket: %w", err)
	}
	return nil
}
