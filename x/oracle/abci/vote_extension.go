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
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

type VoteExtensionHandler struct {
	keeper  Keeper
	enabled bool
	socket  string
	timeout time.Duration
}

func NewVoteExtensionHandler(keeper Keeper, enabled bool, socket string, timeout time.Duration) VoteExtensionHandler {
	return VoteExtensionHandler{
		keeper:  keeper,
		enabled: enabled,
		socket:  strings.TrimSpace(socket),
		timeout: timeout,
	}
}

func (h VoteExtensionHandler) ExtendVote(ctx sdk.Context, req *abcitypes.RequestExtendVote) (*abcitypes.ResponseExtendVote, error) {
	if !h.enabled {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	params, err := h.keeper.GetParams(ctx)
	if err != nil || h.socket == "" {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	tasks, err := h.keeper.DueTasks(ctx, req.Height)
	if err != nil || len(tasks) == 0 {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	response, err := h.fetchSamples(ctx.Context(), tasks, req.Height)
	if err != nil {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	results := validatorResultsFromSamples(tasks, response.GetSymbols(), params.GetMinSources())
	if len(results) == 0 {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	bz, err := proto.Marshal(&oraclev1.OracleVoteExtension{Results: results})
	if err != nil {
		return &abcitypes.ResponseExtendVote{VoteExtension: []byte{}}, nil
	}

	return &abcitypes.ResponseExtendVote{VoteExtension: bz}, nil
}

func (h VoteExtensionHandler) VerifyVoteExtension(_ sdk.Context, req *abcitypes.RequestVerifyVoteExtension) (*abcitypes.ResponseVerifyVoteExtension, error) {
	if len(req.VoteExtension) == 0 {
		return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_ACCEPT}, nil
	}
	if _, err := DecodeVoteExtension(req.VoteExtension); err != nil {
		return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_REJECT}, nil
	}

	return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_ACCEPT}, nil
}

func (h VoteExtensionHandler) fetchSamples(ctx context.Context, tasks []*oraclev1.OracleTask, height int64) (*oraclev1.GetSamplesResponse, error) {
	timeout := h.timeout
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := grpc.NewClient(sidecarTarget(h.socket), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := oraclev1.NewOracleSidecarClient(conn)
	return client.GetSamples(callCtx, &oraclev1.GetSamplesRequest{
		Tasks:  tasks,
		Height: height,
	})
}

func sidecarTarget(socket string) string {
	if strings.HasPrefix(socket, "unix://") {
		return socket
	}

	return "unix://" + socket
}

func validatorResultsFromSamples(
	tasks []*oraclev1.OracleTask,
	sampleGroups []*oraclev1.OracleSymbolSamples,
	minSources uint32,
) []*oraclev1.OracleValidatorResult {
	groupsBySymbol := make(map[string]*oraclev1.OracleSymbolSamples, len(sampleGroups))
	for _, group := range sampleGroups {
		groupsBySymbol[oraclekeeper.NormalizeSymbol(group.GetSymbol())] = group
	}

	results := make([]*oraclev1.OracleValidatorResult, 0, len(tasks))
	for _, task := range tasks {
		symbol := oraclekeeper.NormalizeSymbol(task.GetSymbol())
		group, ok := groupsBySymbol[symbol]
		if !ok {
			continue
		}
		if task.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
			// TODO: add non-numeric aggregation once v1 defines deterministic semantics.
			continue
		}

		medianValue, sourceCount, err := medianFromSamples(group.GetSamples(), task.GetValueType())
		if err != nil || sourceCount < minSources {
			continue
		}
		results = append(results, &oraclev1.OracleValidatorResult{
			Symbol:      symbol,
			ValueType:   task.GetValueType(),
			Value:       medianValue.String(),
			SourceCount: sourceCount,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].GetSymbol() < results[j].GetSymbol()
	})

	return results
}

func medianFromSamples(samples []*oraclev1.OracleSample, valueType oraclev1.ValueType) (sdkmath.LegacyDec, uint32, error) {
	bySource := map[string]sdkmath.LegacyDec{}
	for _, sample := range samples {
		source := strings.TrimSpace(sample.GetSource())
		if source == "" || sample.GetValueType() != valueType {
			continue
		}
		if _, exists := bySource[source]; exists {
			continue
		}
		value, err := sdkmath.LegacyNewDecFromStr(sample.GetValue())
		if err != nil {
			continue
		}
		bySource[source] = value
	}
	if len(bySource) == 0 {
		return sdkmath.LegacyDec{}, 0, fmt.Errorf("no valid numeric samples")
	}

	values := make([]sdkmath.LegacyDec, 0, len(bySource))
	for _, value := range bySource {
		values = append(values, value)
	}

	return median(values), uint32(len(values)), nil
}
