package abci

import (
	"sort"

	sdkmath "cosmossdk.io/math"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/gurufinglobal/guru/v2/config"
	oraclekeeper "github.com/gurufinglobal/guru/v2/x/oracle/keeper"
	oraclev1 "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

func (a Aggregator) aggregateValues(ctx sdk.Context, height int64, extCommit abcitypes.ExtendedCommitInfo) ([]*oraclev1.OracleValue, error) {
	params, err := a.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := a.keeper.DueTasksForVoteExtension(ctx, voteExtensionHeight(height))
	if err != nil {
		return nil, err
	}

	taskBySymbol := make(map[string]*oraclev1.OracleTask, len(tasks))
	for _, task := range tasks {
		taskBySymbol[oraclekeeper.NormalizeSymbol(task.GetSymbol())] = task
	}

	valuesBySymbol := make(map[string][]sdkmath.LegacyDec, len(taskBySymbol))
	for _, vote := range extCommit.GetVotes() {
		if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit || len(vote.VoteExtension) == 0 {
			continue
		}

		extension, err := DecodeVoteExtension(vote.VoteExtension)
		if err != nil {
			return nil, err
		}
		for _, result := range extension.GetResults() {
			symbol := oraclekeeper.NormalizeSymbol(result.GetSymbol())
			task, ok := taskBySymbol[symbol]
			if !ok || task.GetValueType() != oraclev1.ValueType_VALUE_TYPE_NUMERIC {
				continue
			}
			if result.GetSourceCount() < params.GetMinSources() {
				continue
			}

			value, err := sdkmath.LegacyNewDecFromStr(result.GetValue())
			if err != nil {
				return nil, err
			}
			// The min-gas oracle value feeds a denominator in fee policy, so
			// non-positive values are ignored even if they are valid decimals.
			if isMinGasPriceOracleSymbol(symbol) && !value.IsPositive() {
				continue
			}
			valuesBySymbol[symbol] = append(valuesBySymbol[symbol], value)
		}
	}

	symbols := make([]string, 0, len(valuesBySymbol))
	for symbol, values := range valuesBySymbol {
		if uint32(len(values)) >= params.GetMinValidators() {
			symbols = append(symbols, symbol)
		}
	}
	sort.Strings(symbols)

	results := make([]*oraclev1.OracleValue, 0, len(symbols))
	for _, symbol := range symbols {
		task := taskBySymbol[symbol]
		results = append(results, &oraclev1.OracleValue{
			Symbol:        symbol,
			ValueType:     task.GetValueType(),
			Value:         median(valuesBySymbol[symbol]).String(),
			BlockHeight:   height,
			BlockTimeUnix: ctx.BlockTime().Unix(),
		})
	}

	return results, nil
}

func voteExtensionHeight(proposalHeight int64) int64 {
	return proposalHeight - 1
}

func isMinGasPriceOracleSymbol(symbol string) bool {
	return oraclekeeper.NormalizeSymbol(symbol) == oraclekeeper.NormalizeSymbol(appparams.MinGasPriceOracleSymbol)
}
