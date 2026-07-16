package oracle

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	nodeTasksPageLimit      uint64 = 100
	initialPreflightBackoff        = 250 * time.Millisecond
	maximumPreflightBackoff        = 5 * time.Second
)

type nodeTaskPreflight struct {
	config         Config
	sidecar        *Sidecar
	ensure         func(context.Context, Config) ([]*oraclev1.OracleTask, error)
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

func NewNodeTaskPreflight(config Config, sidecar *Sidecar) Runnable {
	config.applyDefaults("")

	return &nodeTaskPreflight{
		config:         config,
		sidecar:        sidecar,
		ensure:         EnsureNodeTasksConfigured,
		initialBackoff: initialPreflightBackoff,
		maxBackoff:     maximumPreflightBackoff,
	}
}

func (p *nodeTaskPreflight) Run(ctx context.Context) error {
	backoff := p.initialBackoff
	for attempt := uint64(1); ; attempt++ {
		tasks, err := p.ensure(ctx, p.config)
		if err == nil {
			if err := p.sidecar.ConfigureActiveTasks(tasks); err != nil {
				return err
			}
			log.Printf(
				"oracle sidecar node_preflight=ready attempts=%d active_tasks=%d node_grpc=%q",
				attempt,
				len(tasks),
				strings.TrimSpace(p.config.NodeGRPC),
			)

			return nil
		}
		if ctx.Err() != nil {
			return nil
		}

		log.Printf(
			"oracle sidecar node_preflight=degraded attempt=%d retry_in=%s node_grpc=%q err=%v",
			attempt,
			backoff,
			strings.TrimSpace(p.config.NodeGRPC),
			err,
		)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}

		if backoff < p.maxBackoff {
			backoff *= 2
			if backoff > p.maxBackoff {
				backoff = p.maxBackoff
			}
		}
	}
}

func EnsureNodeTasksConfigured(ctx context.Context, cfg Config) ([]*oraclev1.OracleTask, error) {
	cfg.applyDefaults("")
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	timeout, err := cfg.NodeQueryTimeoutDuration()
	if err != nil {
		return nil, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := grpc.NewClient(strings.TrimSpace(cfg.NodeGRPC), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	client := oraclev1.NewQueryClient(conn)
	paramsResp, err := client.Params(queryCtx, &oraclev1.QueryParamsRequest{})
	if err != nil {
		return nil, err
	}
	if paramsResp.GetParams() == nil {
		return nil, fmt.Errorf("oracle params are not initialized on node %s", cfg.NodeGRPC)
	}

	tasks, err := queryActiveTasks(queryCtx, client)
	if err != nil {
		return nil, err
	}
	tasks = normalizedTasks(tasks)
	if len(tasks) == 0 {
		return nil, fmt.Errorf("oracle module has no active numeric tasks on node %s", cfg.NodeGRPC)
	}
	matches := MatchingSourcesForTasks(cfg.Sources, tasks)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no configured oracle sources match active node oracle tasks")
	}
	if !hasSourceQuorum(matches, paramsResp.GetParams().GetMinSources()) {
		return nil, fmt.Errorf("configured oracle sources do not satisfy min_sources=%d for any active node oracle task", paramsResp.GetParams().GetMinSources())
	}

	return tasks, nil
}

func queryActiveTasks(ctx context.Context, client oraclev1.QueryClient) ([]*oraclev1.OracleTask, error) {
	var tasks []*oraclev1.OracleTask
	pagination := &queryv1beta1.PageRequest{Limit: nodeTasksPageLimit}
	for {
		tasksResp, err := client.ActiveTasks(ctx, &oraclev1.QueryActiveTasksRequest{Pagination: pagination})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, tasksResp.GetTasks()...)

		nextKey := tasksResp.GetPagination().GetNextKey()
		if len(nextKey) == 0 {
			return tasks, nil
		}
		pagination = &queryv1beta1.PageRequest{
			Key:   nextKey,
			Limit: nodeTasksPageLimit,
		}
	}
}

func MatchingSourcesForTasks(sources []SourceConfig, tasks []*oraclev1.OracleTask) []SourceConfig {
	taskTypes := make(map[string]oraclev1.ValueType, len(tasks))
	for _, task := range normalizedTasks(tasks) {
		taskTypes[normalizeSymbol(task.GetSymbol())] = task.GetValueType()
	}

	matches := make([]SourceConfig, 0)
	seen := map[string]struct{}{}
	for _, source := range sources {
		symbol := normalizeSymbol(source.Symbol)
		taskType, ok := taskTypes[symbol]
		if !ok {
			continue
		}
		sourceType, err := source.ProtoValueType()
		if err != nil || sourceType != taskType {
			continue
		}

		key := symbol + "\x00" + strings.TrimSpace(source.Name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		matches = append(matches, source)
	}

	sort.Slice(matches, func(i, j int) bool {
		left := normalizeSymbol(matches[i].Symbol) + "\x00" + strings.TrimSpace(matches[i].Name)
		right := normalizeSymbol(matches[j].Symbol) + "\x00" + strings.TrimSpace(matches[j].Name)
		return left < right
	})

	return matches
}

func hasSourceQuorum(sources []SourceConfig, minSources uint32) bool {
	if minSources == 0 {
		return true
	}

	counts := make(map[string]uint32, len(sources))
	for _, source := range sources {
		symbol := normalizeSymbol(source.Symbol)
		counts[symbol]++
		if counts[symbol] >= minSources {
			return true
		}
	}

	return false
}

func normalizedTasks(tasks []*oraclev1.OracleTask) []*oraclev1.OracleTask {
	bySymbol := make(map[string]*oraclev1.OracleTask, len(tasks))
	for _, task := range tasks {
		symbol := normalizeSymbol(task.GetSymbol())
		if symbol == "" || !task.GetEnabled() || task.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
			continue
		}
		bySymbol[symbol] = &oraclev1.OracleTask{
			Symbol:             symbol,
			ValueType:          task.GetValueType(),
			Enabled:            true,
			SubmissionInterval: task.GetSubmissionInterval(),
		}
	}

	symbols := make([]string, 0, len(bySymbol))
	for symbol := range bySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	result := make([]*oraclev1.OracleTask, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, bySymbol[symbol])
	}

	return result
}
