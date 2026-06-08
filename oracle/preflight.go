package oracle

import (
	"context"
	"fmt"
	"sort"
	"strings"

	queryv1beta1 "cosmossdk.io/api/cosmos/base/query/v1beta1"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const nodeTasksPageLimit uint64 = 100

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
	defer conn.Close()

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
		return nil, fmt.Errorf("oracle module has no active tasks on node %s", cfg.NodeGRPC)
	}
	if len(MatchingSourcesForTasks(cfg.Sources, tasks)) == 0 {
		return nil, fmt.Errorf("no configured oracle sources match active node oracle tasks")
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

func normalizedTasks(tasks []*oraclev1.OracleTask) []*oraclev1.OracleTask {
	bySymbol := make(map[string]*oraclev1.OracleTask, len(tasks))
	for _, task := range tasks {
		symbol := normalizeSymbol(task.GetSymbol())
		if symbol == "" || !task.GetEnabled() || task.GetValueType() == oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED {
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
