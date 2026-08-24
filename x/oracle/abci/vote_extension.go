package abci

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclekeeper "github.com/gurufinglobal/guru/v2/x/oracle/keeper"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type VoteExtensionHandler struct {
	keeper  Keeper
	enabled bool
	socket  string
	timeout time.Duration
	conn    *grpc.ClientConn
	client  oraclev1.OracleSidecarClient
}

const (
	defaultSidecarTimeout  = 200 * time.Millisecond
	maxSidecarSymbols      = 256
	maxSidecarSymbolBytes  = 128
	maxSidecarValueBytes   = 256
	maxSidecarRequestBytes = 64 << 10
	maxSidecarMessageBytes = 1 << 20
)

func NewVoteExtensionHandler(
	keeper Keeper,
	enabled bool,
	socket string,
	timeout time.Duration,
) (*VoteExtensionHandler, error) {
	handler := &VoteExtensionHandler{
		keeper:  keeper,
		enabled: enabled,
		socket:  strings.TrimSpace(socket),
		timeout: timeout,
	}
	if !handler.enabled || handler.socket == "" {
		return handler, nil
	}

	conn, err := grpc.NewClient(
		sidecarTarget(handler.socket),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxSidecarMessageBytes),
			grpc.MaxCallSendMsgSize(maxSidecarMessageBytes),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create oracle sidecar client: %w", err)
	}
	handler.conn = conn
	handler.client = oraclev1.NewOracleSidecarClient(conn)

	return handler, nil
}

func (h *VoteExtensionHandler) Close() error {
	if h == nil || h.conn == nil {
		return nil
	}

	return h.conn.Close()
}

func (h *VoteExtensionHandler) ExtendVote(ctx sdk.Context, req *abcitypes.RequestExtendVote) (*abcitypes.ResponseExtendVote, error) {
	if !h.enabled {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}
	if h.client == nil {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	tasks, err := h.keeper.DueTasksForVoteExtension(ctx, req.Height)
	if err != nil || len(tasks) == 0 {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	symbols, tasksBySymbol, ok := aggregateRequestFromTasks(tasks)
	if !ok || len(symbols) == 0 {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	request := &oraclev1.GetAggregatesRequest{Symbols: symbols}
	if request.Size() > maxSidecarRequestBytes {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	response, err := h.fetchAggregates(ctx.Context(), request)
	if err != nil {
		// Local sidecar failure must not stop consensus. Missing validator
		// results simply reduce oracle quorum for this height.
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}
	if len(response.GetResults()) == 0 || len(response.GetResults()) > maxSidecarSymbols {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}
	params, err := h.keeper.GetParams(ctx)
	if err != nil {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	results := validatorResultsFromAggregates(tasksBySymbol, response.GetResults(), params.GetMinSources())
	if len(results) == 0 {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	bz, err := (&oraclev1.OracleVoteExtension{Results: results}).Marshal()
	if err != nil {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	return &abcitypes.ResponseExtendVote{VoteExtension: bz}, nil
}

func (h *VoteExtensionHandler) VerifyVoteExtension(_ sdk.Context, req *abcitypes.RequestVerifyVoteExtension) (*abcitypes.ResponseVerifyVoteExtension, error) {
	if len(req.VoteExtension) == 0 {
		return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_ACCEPT}, nil
	}
	if _, err := DecodeVoteExtension(req.VoteExtension); err != nil {
		return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_REJECT}, nil
	}

	return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_ACCEPT}, nil
}

func (h *VoteExtensionHandler) fetchAggregates(
	ctx context.Context,
	request *oraclev1.GetAggregatesRequest,
) (*oraclev1.GetAggregatesResponse, error) {
	if h.client == nil {
		return nil, fmt.Errorf("oracle sidecar client is unavailable")
	}

	timeout := h.timeout
	if timeout <= 0 {
		timeout = defaultSidecarTimeout
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return h.client.GetAggregates(callCtx, request)
}

func sidecarTarget(socket string) string {
	if strings.HasPrefix(socket, "unix://") {
		return socket
	}

	return "unix://" + socket
}

func aggregateRequestFromTasks(
	tasks []*oraclev1.OracleTask,
) ([]string, map[string]*oraclev1.OracleTask, bool) {
	tasksBySymbol := make(
		map[string]*oraclev1.OracleTask,
		min(len(tasks), maxSidecarSymbols+1),
	)
	for _, task := range tasks {
		if task == nil || task.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
			continue
		}

		symbol := oraclekeeper.NormalizeSymbol(task.GetSymbol())
		if symbol == "" || len(symbol) > maxSidecarSymbolBytes {
			continue
		}
		if _, exists := tasksBySymbol[symbol]; exists {
			continue
		}
		if len(tasksBySymbol) == maxSidecarSymbols {
			return nil, nil, false
		}
		tasksBySymbol[symbol] = task
	}

	symbols := make([]string, 0, len(tasksBySymbol))
	for symbol := range tasksBySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	return symbols, tasksBySymbol, true
}

func validatorResultsFromAggregates(
	tasksBySymbol map[string]*oraclev1.OracleTask,
	aggregates []*oraclev1.AggregatedResult,
	minSources uint32,
) []*oraclev1.OracleValidatorResult {
	seen := make(map[string]struct{}, len(aggregates))
	duplicates := make(map[string]struct{})
	accepted := make(map[string]*oraclev1.OracleValidatorResult, len(aggregates))
	for _, aggregate := range aggregates {
		symbol := oraclekeeper.NormalizeSymbol(aggregate.GetSymbol())
		task, requested := tasksBySymbol[symbol]
		if !requested {
			continue
		}
		if _, exists := seen[symbol]; exists {
			duplicates[symbol] = struct{}{}
			delete(accepted, symbol)
			continue
		}
		seen[symbol] = struct{}{}

		if aggregate == nil ||
			aggregate.GetSymbol() != symbol ||
			len(symbol) > maxSidecarSymbolBytes ||
			task.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC ||
			aggregate.GetSourceCount() == 0 ||
			aggregate.GetSourceCount() < minSources {
			continue
		}

		rawValue := aggregate.GetValue()
		if rawValue == "" || len(rawValue) > maxSidecarValueBytes {
			continue
		}
		value, err := sdkmath.LegacyNewDecFromStr(rawValue)
		if err != nil || value.String() != rawValue {
			continue
		}

		accepted[symbol] = &oraclev1.OracleValidatorResult{
			Symbol:      symbol,
			Value:       rawValue,
			SourceCount: aggregate.GetSourceCount(),
		}
	}

	results := make([]*oraclev1.OracleValidatorResult, 0, len(accepted))
	for symbol, result := range accepted {
		if _, duplicate := duplicates[symbol]; duplicate {
			continue
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].GetSymbol() < results[j].GetSymbol()
	})

	return results
}
