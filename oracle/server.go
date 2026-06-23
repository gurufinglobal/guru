package oracle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Sidecar struct {
	oraclev1.UnimplementedOracleSidecarServer

	config      Config
	client      *HTTPSourceClient
	activeTasks []*oraclev1.OracleTask

	cacheMu     sync.RWMutex
	sourceCache map[string]cachedSample
	fetchGroup  singleflight.Group
}

type cachedSample struct {
	sample    *oraclev1.OracleSample
	fetchedAt time.Time
}

func NewSidecar(config Config, activeTasks ...[]*oraclev1.OracleTask) (*Sidecar, error) {
	config.applyDefaults("")
	if err := config.Validate(); err != nil {
		return nil, err
	}

	tasks := []*oraclev1.OracleTask{}
	if len(activeTasks) > 0 {
		tasks = normalizedTasks(activeTasks[0])
	}

	return &Sidecar{
		config:      config,
		client:      NewHTTPSourceClient(),
		activeTasks: tasks,
		sourceCache: map[string]cachedSample{},
	}, nil
}

func (s *Sidecar) Run(ctx context.Context) error {
	socketPath := SocketPath(s.config.Socket)
	if socketPath == "" {
		return fmt.Errorf("socket is required")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return err
	}
	if err := removeStaleSocket(socketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := grpc.NewServer()
	oraclev1.RegisterOracleSidecarServer(server, s)
	log.Printf(
		"oracle sidecar serving socket=%q active_tasks=%d configured_sources=%d",
		socketPath,
		len(s.activeTasks),
		len(s.config.Sources),
	)

	group, runCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		<-runCtx.Done()
		server.GracefulStop()
		return nil
	})
	group.Go(func() error {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}

		return nil
	})
	if len(s.activeTasks) != 0 {
		group.Go(func() error {
			return s.runSourcePollers(runCtx)
		})
	}

	return group.Wait()
}

func (s *Sidecar) GetSamples(ctx context.Context, req *oraclev1.GetSamplesRequest) (*oraclev1.GetSamplesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tasks := normalizedTasks(req.GetTasks())
	log.Printf("oracle sidecar received sample request height=%d tasks=%d", req.GetHeight(), len(tasks))

	timeout, err := s.config.RequestTimeoutDuration()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sourceTimeout, err := s.config.SourceTimeoutDuration()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	type sampleResult struct {
		symbol string
		sample *oraclev1.OracleSample
	}

	results := make([]sampleResult, 0)
	var mu sync.Mutex
	group, groupCtx := errgroup.WithContext(requestCtx)

	for _, task := range tasks {
		for _, source := range s.matchingSources(task) {
			task := task
			source := source
			group.Go(func() error {
				sample, err := s.sampleForSource(groupCtx, source, task, sourceTimeout)
				if err != nil {
					log.Printf(
						"oracle sidecar sample fetch failed height=%d symbol=%q source=%q err=%v",
						req.GetHeight(),
						normalizeSymbol(task.GetSymbol()),
						strings.TrimSpace(source.Name),
						err,
					)
					return nil
				}
				log.Printf(
					"oracle sidecar collected sample height=%d symbol=%q source=%q value=%q",
					req.GetHeight(),
					normalizeSymbol(task.GetSymbol()),
					sample.GetSource(),
					sample.GetValue(),
				)

				mu.Lock()
				results = append(results, sampleResult{symbol: normalizeSymbol(task.GetSymbol()), sample: sample})
				mu.Unlock()

				return nil
			})
		}
	}

	if err := group.Wait(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	grouped := map[string][]*oraclev1.OracleSample{}
	for _, result := range results {
		grouped[result.symbol] = append(grouped[result.symbol], result.sample)
	}

	symbols := make([]string, 0, len(grouped))
	for symbol := range grouped {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	response := &oraclev1.GetSamplesResponse{Symbols: make([]*oraclev1.OracleSymbolSamples, 0, len(symbols))}
	sampleCount := 0
	for _, symbol := range symbols {
		samples := grouped[symbol]
		sampleCount += len(samples)
		sort.Slice(samples, func(i, j int) bool {
			return samples[i].GetSource() < samples[j].GetSource()
		})
		response.Symbols = append(response.Symbols, &oraclev1.OracleSymbolSamples{
			Symbol:  symbol,
			Samples: samples,
		})
	}
	log.Printf(
		"oracle sidecar served sample request height=%d symbols=%d samples=%d",
		req.GetHeight(),
		len(response.GetSymbols()),
		sampleCount,
	)

	return response, nil
}

func (s *Sidecar) matchingSources(task *oraclev1.OracleTask) []SourceConfig {
	return MatchingSourcesForTasks(s.config.Sources, []*oraclev1.OracleTask{task})
}

func (s *Sidecar) runSourcePollers(ctx context.Context) error {
	sourceTimeout, err := s.config.SourceTimeoutDuration()
	if err != nil {
		return err
	}

	group, pollCtx := errgroup.WithContext(ctx)
	pollers := 0
	for _, task := range s.activeTasks {
		for _, source := range s.matchingSources(task) {
			if _, ok, err := source.IntervalDuration(); err != nil {
				return err
			} else if !ok {
				continue
			}

			task := task
			source := source
			group.Go(func() error {
				return s.pollSource(pollCtx, source, task, sourceTimeout)
			})
			pollers++
		}
	}
	log.Printf("oracle sidecar source pollers started count=%d", pollers)

	return group.Wait()
}

func (s *Sidecar) pollSource(ctx context.Context, source SourceConfig, task *oraclev1.OracleTask, sourceTimeout time.Duration) error {
	interval, ok, err := source.IntervalDuration()
	if err != nil || !ok {
		return err
	}
	log.Printf(
		"oracle sidecar source poller started symbol=%q source=%q interval=%s",
		normalizeSymbol(task.GetSymbol()),
		strings.TrimSpace(source.Name),
		interval,
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			sample, err := s.fetchAndCache(ctx, source, task, sourceTimeout)
			if err != nil {
				log.Printf(
					"oracle sidecar source poll failed symbol=%q source=%q err=%v",
					normalizeSymbol(task.GetSymbol()),
					strings.TrimSpace(source.Name),
					err,
				)
			} else {
				log.Printf(
					"oracle sidecar source polled symbol=%q source=%q value=%q",
					normalizeSymbol(task.GetSymbol()),
					sample.GetSource(),
					sample.GetValue(),
				)
			}
			timer.Reset(interval)
		}
	}
}

func (s *Sidecar) sampleForSource(ctx context.Context, source SourceConfig, task *oraclev1.OracleTask, sourceTimeout time.Duration) (*oraclev1.OracleSample, error) {
	interval, ok, err := source.IntervalDuration()
	if err != nil {
		return nil, err
	}
	if ok {
		if sample, found := s.cachedSourceSample(source, task, interval); found {
			return sample, nil
		}

		return s.fetchAndCache(ctx, source, task, sourceTimeout)
	}

	return s.client.Fetch(ctx, source, task, sourceTimeout)
}

func (s *Sidecar) cachedSourceSample(source SourceConfig, task *oraclev1.OracleTask, interval time.Duration) (*oraclev1.OracleSample, bool) {
	key := sourceCacheKey(source, task)
	now := s.client.now()

	s.cacheMu.RLock()
	cached, found := s.sourceCache[key]
	s.cacheMu.RUnlock()
	if !found || now.Sub(cached.fetchedAt) > interval {
		return nil, false
	}

	return cloneOracleSample(cached.sample), true
}

func (s *Sidecar) fetchAndCache(ctx context.Context, source SourceConfig, task *oraclev1.OracleTask, sourceTimeout time.Duration) (*oraclev1.OracleSample, error) {
	key := sourceCacheKey(source, task)
	result, err, _ := s.fetchGroup.Do(key, func() (any, error) {
		return s.client.Fetch(ctx, source, task, sourceTimeout)
	})
	if err != nil {
		return nil, err
	}
	sample := result.(*oraclev1.OracleSample)

	s.cacheMu.Lock()
	s.sourceCache[key] = cachedSample{
		sample:    cloneOracleSample(sample),
		fetchedAt: s.client.now(),
	}
	s.cacheMu.Unlock()

	return cloneOracleSample(sample), nil
}

func sourceCacheKey(source SourceConfig, task *oraclev1.OracleTask) string {
	return normalizeSymbol(task.GetSymbol()) + "\x00" + strings.TrimSpace(source.Name)
}

func cloneOracleSample(sample *oraclev1.OracleSample) *oraclev1.OracleSample {
	if sample == nil {
		return nil
	}

	cloned := *sample
	return &cloned
}

func SocketPath(socket string) string {
	socket = strings.TrimSpace(socket)
	if strings.HasPrefix(socket, "unix://") {
		return strings.TrimPrefix(socket, "unix://")
	}

	return socket
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket file at %s", path)
	}

	return os.Remove(path)
}
