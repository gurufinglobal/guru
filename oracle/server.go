package oracle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Sidecar struct {
	oraclev1.UnimplementedOracleSidecarServer

	config Config
	client *HTTPSourceClient
}

func NewSidecar(config Config) (*Sidecar, error) {
	config.applyDefaults("")
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Sidecar{
		config: config,
		client: NewHTTPSourceClient(),
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

	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()

	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}

	return nil
}

func (s *Sidecar) GetSamples(ctx context.Context, req *oraclev1.GetSamplesRequest) (*oraclev1.GetSamplesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

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

	for _, task := range normalizedTasks(req.GetTasks()) {
		for _, source := range s.matchingSources(task) {
			task := task
			source := source
			group.Go(func() error {
				sample, err := s.client.Fetch(groupCtx, source, task, sourceTimeout)
				if err != nil {
					return nil
				}

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
	for _, symbol := range symbols {
		samples := grouped[symbol]
		sort.Slice(samples, func(i, j int) bool {
			return samples[i].GetSource() < samples[j].GetSource()
		})
		response.Symbols = append(response.Symbols, &oraclev1.OracleSymbolSamples{
			Symbol:  symbol,
			Samples: samples,
		})
	}

	return response, nil
}

func (s *Sidecar) matchingSources(task *oraclev1.OracleTask) []SourceConfig {
	return MatchingSourcesForTasks(s.config.Sources, []*oraclev1.OracleTask{task})
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
