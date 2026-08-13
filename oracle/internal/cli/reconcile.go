package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/service"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ReconcileData struct {
	NodeGRPC        string    `json:"node_grpc"`
	MinSources      uint32    `json:"min_sources"`
	ActiveTaskCount uint32    `json:"active_task_count"`
	Findings        []Finding `json:"findings"`
}

type Finding struct {
	Code     string  `json:"code"`
	Blocking bool    `json:"blocking"`
	Symbol   *string `json:"symbol"`
	Message  string  `json:"message"`
}

type reconcileProtocolError struct {
	err error
}

func (e *reconcileProtocolError) Error() string { return e.err.Error() }
func (e *reconcileProtocolError) Unwrap() error { return e.err }

func asReconcileProtocolError(err error) error {
	return &reconcileProtocolError{err: err}
}

func isReconcileProtocolError(err error) bool {
	var protocolError *reconcileProtocolError
	return errors.As(err, &protocolError)
}

func reconcile(
	ctx context.Context,
	nodeEndpoint string,
	status service.StatusData,
	expectedPublicationRevision string,
	expectedSourcesSHA256 string,
) (result ReconcileData, resultErr error) {
	if nodeEndpoint == "" {
		return ReconcileData{}, errors.New("--node-grpc is required")
	}
	connection, err := grpc.NewClient(
		nodeEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(64<<10)),
	)
	if err != nil {
		return ReconcileData{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, connection.Close())
	}()
	client := oraclev1.NewQueryClient(connection)
	queryContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	paramsResponse, err := client.Params(queryContext, &oraclev1.QueryParamsRequest{})
	if err != nil {
		return ReconcileData{}, fmt.Errorf("query node params: %w", err)
	}
	if paramsResponse == nil || paramsResponse.Params == nil {
		return ReconcileData{}, asReconcileProtocolError(errors.New("node params response is missing params"))
	}
	tasks, err := queryAllTasks(queryContext, client)
	if err != nil {
		return ReconcileData{}, err
	}
	data := ReconcileData{
		NodeGRPC:        nodeEndpoint,
		MinSources:      paramsResponse.Params.MinSources,
		ActiveTaskCount: uint32(len(tasks)),
		Findings:        []Finding{},
	}
	if status.PublicationRevision != expectedPublicationRevision ||
		status.SourcesSHA256 != expectedSourcesSHA256 {
		data.Findings = append(data.Findings, Finding{
			Code:     "runtime_config_mismatch",
			Blocking: true,
			Symbol:   nil,
			Message:  "Running sidecar configuration does not match the published configuration pair.",
		})
	}
	local := make(map[string]service.FeedStatus, len(status.Feeds))
	for _, feed := range status.Feeds {
		normalized, normalizeErr := domain.NormalizeSymbol(feed.Symbol)
		if normalizeErr != nil || normalized != feed.Symbol {
			return ReconcileData{}, asReconcileProtocolError(errors.New("sidecar status contains a non-canonical symbol"))
		}
		if _, exists := local[feed.Symbol]; exists {
			return ReconcileData{}, asReconcileProtocolError(errors.New("sidecar status contains a duplicate symbol"))
		}
		local[feed.Symbol] = feed
	}
	active := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		symbol := task.Symbol
		if _, exists := active[symbol]; exists {
			return ReconcileData{}, asReconcileProtocolError(errors.New("node returned a duplicate active task symbol"))
		}
		active[symbol] = struct{}{}
		if task.ValueType != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
			data.Findings = append(data.Findings, finding("unsupported_task_type", true, symbol, "Active task is not numeric."))
			continue
		}
		feed, ok := local[symbol]
		if !ok {
			data.Findings = append(data.Findings, finding("missing_symbol", true, symbol, "Active task is absent from sidecar configuration."))
			continue
		}
		if feed.ConfiguredSourceCount < domain.MinSourcesPerFeed || feed.ConfiguredSourceCount < data.MinSources {
			data.Findings = append(data.Findings, finding(
				"configured_sources_below_minimum",
				true,
				symbol,
				"Configured source count is below the required minimum.",
			))
		}
		switch feed.Freshness {
		case "no_value":
			data.Findings = append(data.Findings, finding("no_value", true, symbol, "No current-plan aggregate is available."))
		case "stale":
			data.Findings = append(data.Findings, finding("stale", true, symbol, "Latest aggregate is stale."))
		case "clock_anomaly":
			data.Findings = append(data.Findings, finding("clock_anomaly", true, symbol, "Latest aggregate has a clock anomaly."))
		case "fresh":
			if feed.Latest == nil {
				return ReconcileData{}, asReconcileProtocolError(errors.New("fresh feed is missing latest aggregate metadata"))
			}
			if feed.Latest.SuccessfulSourceCount < data.MinSources {
				data.Findings = append(data.Findings, finding(
					"aggregate_sources_below_minimum",
					true,
					symbol,
					"Latest aggregate source count is below the on-chain minimum.",
				))
			}
			if feed.Cycle.LastOutcome == string(domain.CycleUnderQuorum) {
				data.Findings = append(data.Findings, finding(
					"under_quorum",
					false,
					symbol,
					"Latest cycle was under quorum while an older aggregate remains fresh.",
				))
			}
		default:
			return ReconcileData{}, asReconcileProtocolError(errors.New("sidecar returned an unsupported freshness value"))
		}
	}
	for symbol := range local {
		if _, ok := active[symbol]; !ok {
			data.Findings = append(data.Findings, finding(
				"inactive_symbol",
				false,
				symbol,
				"Configured sidecar symbol is not an active on-chain task.",
			))
		}
	}
	sort.Slice(data.Findings, func(i, j int) bool {
		if data.Findings[i].Blocking != data.Findings[j].Blocking {
			return data.Findings[i].Blocking
		}
		if data.Findings[i].Code != data.Findings[j].Code {
			return data.Findings[i].Code < data.Findings[j].Code
		}
		left, right := "", ""
		if data.Findings[i].Symbol != nil {
			left = *data.Findings[i].Symbol
		}
		if data.Findings[j].Symbol != nil {
			right = *data.Findings[j].Symbol
		}
		return left < right
	})
	return data, nil
}

func queryAllTasks(ctx context.Context, client oraclev1.QueryClient) ([]*oraclev1.OracleTask, error) {
	var (
		key   []byte
		tasks []*oraclev1.OracleTask
	)
	seenKeys := make(map[string]struct{})
	for page := 0; page < 256; page++ {
		response, err := client.ActiveTasks(ctx, &oraclev1.QueryActiveTasksRequest{
			Pagination: &sdkquery.PageRequest{Key: key, Limit: 100},
		})
		if err != nil {
			return nil, fmt.Errorf("query active tasks: %w", err)
		}
		if response == nil {
			return nil, asReconcileProtocolError(errors.New("node returned a nil active task response"))
		}
		if len(response.Tasks) > 4096-len(tasks) {
			return nil, asReconcileProtocolError(errors.New("node active task response exceeds limit"))
		}
		for _, task := range response.Tasks {
			if task == nil {
				return nil, asReconcileProtocolError(errors.New("node returned a nil active task"))
			}
			symbol, normalizeErr := domain.NormalizeSymbol(task.Symbol)
			if normalizeErr != nil || symbol != task.Symbol {
				return nil, asReconcileProtocolError(errors.New("node returned a non-canonical active task symbol"))
			}
			tasks = append(tasks, task)
		}
		if response.Pagination == nil || len(response.Pagination.NextKey) == 0 {
			return tasks, nil
		}
		next := string(response.Pagination.NextKey)
		if _, exists := seenKeys[next]; exists {
			return nil, asReconcileProtocolError(errors.New("node pagination key repeated"))
		}
		seenKeys[next] = struct{}{}
		key = append(key[:0], response.Pagination.NextKey...)
	}
	return nil, asReconcileProtocolError(errors.New("node active task pagination exceeds limit"))
}

func finding(code string, blocking bool, symbol, message string) Finding {
	symbolCopy := symbol
	return Finding{Code: code, Blocking: blocking, Symbol: &symbolCopy, Message: message}
}
