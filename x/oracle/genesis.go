package oracle

import (
	"context"
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	oraclekeeper "github.com/gurufinglobal/guru/v3/x/oracle/keeper"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

func (am AppModule) DefaultGenesis(target appmodule.GenesisTarget) error {
	return writeGenesisState(target, am.defaultGenesisState())
}

func (am AppModule) ValidateGenesis(source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	return am.validateGenesisState(genesis)
}

func (am AppModule) InitGenesis(ctx context.Context, source appmodule.GenesisSource) error {
	genesis, err := readGenesisState(source, am.defaultGenesisState())
	if err != nil {
		return err
	}
	if err := am.validateGenesisState(genesis); err != nil {
		return err
	}
	if err := am.keeper.SetParams(ctx, genesis.Params); err != nil {
		return err
	}
	for _, task := range genesis.GetTasks() {
		if err := am.keeper.SetTaskDefinition(ctx, task); err != nil {
			return err
		}
	}
	for _, schedule := range genesis.GetTaskSchedule() {
		if err := am.keeper.SetTaskSchedule(ctx, schedule); err != nil {
			return err
		}
	}
	for _, value := range genesis.GetLatestValues() {
		if err := am.keeper.SetLatestValue(ctx, value); err != nil {
			return err
		}
	}
	for _, history := range genesis.GetHistory() {
		if err := am.keeper.SetHistory(ctx, history, genesis.GetParams().GetHistoryLimit()); err != nil {
			return err
		}
	}

	return nil
}

func (am AppModule) ExportGenesis(ctx context.Context, target appmodule.GenesisTarget) error {
	params, err := am.keeper.GetParams(ctx)
	if err != nil {
		return err
	}
	tasks, err := am.keeper.ListTasks(ctx, false)
	if err != nil {
		return err
	}
	taskSchedule, err := am.keeper.ListTaskSchedule(ctx)
	if err != nil {
		return err
	}
	latestValues, err := am.keeper.ListLatestValues(ctx)
	if err != nil {
		return err
	}
	history, err := am.keeper.ListHistory(ctx)
	if err != nil {
		return err
	}

	return writeGenesisState(target, &oraclev1.GenesisState{
		Params:       params,
		Tasks:        tasks,
		TaskSchedule: taskSchedule,
		LatestValues: latestValues,
		History:      history,
	})
}

func (am AppModule) defaultGenesisState() *oraclev1.GenesisState {
	return &oraclev1.GenesisState{
		Params: oraclekeeper.DefaultParams(),
	}
}

func (am AppModule) validateGenesisState(data *oraclev1.GenesisState) error {
	if data == nil {
		return oracletypes.ErrInvalidParams.Wrap("genesis state cannot be nil")
	}
	if err := oraclekeeper.ValidateParams(data.GetParams()); err != nil {
		return err
	}

	seenTasks := map[string]*oraclev1.OracleTask{}
	scheduledTaskCounts := map[string]int{}
	schedulePhase := map[string]int64{}
	for _, task := range data.GetTasks() {
		if err := oraclekeeper.ValidateTask(task); err != nil {
			return err
		}
		symbol := oraclekeeper.NormalizeSymbol(task.GetSymbol())
		if _, ok := seenTasks[symbol]; ok {
			return oracletypes.ErrInvalidTask.Wrapf("duplicate task symbol %q", symbol)
		}
		seenTasks[symbol] = task
		if task.GetEnabled() {
			scheduledTaskCounts[symbol] = 0
		}
	}

	type taskScheduleKey struct {
		height int64
		symbol string
	}
	seenTaskSchedule := map[taskScheduleKey]struct{}{}
	for _, entry := range data.GetTaskSchedule() {
		if err := oraclekeeper.ValidateTaskScheduleEntry(entry); err != nil {
			return err
		}
		symbol := oraclekeeper.NormalizeSymbol(entry.GetSymbol())
		key := taskScheduleKey{height: entry.GetHeight(), symbol: symbol}
		if _, ok := seenTaskSchedule[key]; ok {
			return oracletypes.ErrInvalidTask.Wrapf("duplicate task schedule for %q at height %d", symbol, entry.GetHeight())
		}
		seenTaskSchedule[key] = struct{}{}

		task, ok := seenTasks[symbol]
		if !ok {
			return oracletypes.ErrInvalidTask.Wrapf("task schedule references unknown task %q", symbol)
		}
		if !task.GetEnabled() {
			return oracletypes.ErrInvalidTask.Wrapf("task schedule references disabled task %q", symbol)
		}

		interval := int64(task.GetSubmissionInterval())
		if firstHeight, ok := schedulePhase[symbol]; ok {
			if (entry.GetHeight()-firstHeight)%interval != 0 {
				return oracletypes.ErrInvalidTask.Wrapf("task schedule for %q is not aligned to submission_interval", symbol)
			}
		} else {
			schedulePhase[symbol] = entry.GetHeight()
		}
		scheduledTaskCounts[symbol]++
	}
	for _, task := range data.GetTasks() {
		if !task.GetEnabled() {
			continue
		}
		symbol := oraclekeeper.NormalizeSymbol(task.GetSymbol())
		if scheduledTaskCounts[symbol] < 2 {
			return oracletypes.ErrInvalidTask.Wrapf("enabled task %q must have at least two schedule entries", symbol)
		}
	}

	seenLatest := map[string]struct{}{}
	for _, value := range data.GetLatestValues() {
		if err := oraclekeeper.ValidateOracleValue(value); err != nil {
			return err
		}
		symbol := oraclekeeper.NormalizeSymbol(value.GetSymbol())
		if _, ok := seenLatest[symbol]; ok {
			return oracletypes.ErrInvalidValue.Wrapf("duplicate latest value symbol %q", symbol)
		}
		seenLatest[symbol] = struct{}{}
	}

	seenHistory := map[string]struct{}{}
	for _, history := range data.GetHistory() {
		if err := oraclekeeper.ValidateHistory(history, data.GetParams().GetHistoryLimit()); err != nil {
			return err
		}
		symbol := oraclekeeper.NormalizeSymbol(history.GetSymbol())
		if _, ok := seenHistory[symbol]; ok {
			return oracletypes.ErrInvalidValue.Wrapf("duplicate history symbol %q", symbol)
		}
		seenHistory[symbol] = struct{}{}
	}

	return nil
}

func readGenesisState(source appmodule.GenesisSource, defaults *oraclev1.GenesisState) (*oraclev1.GenesisState, error) {
	genesis := &oraclev1.GenesisState{
		Params:       defaults.Params,
		Tasks:        defaults.Tasks,
		TaskSchedule: defaults.TaskSchedule,
		LatestValues: defaults.LatestValues,
		History:      defaults.History,
	}

	params := &oraclev1.Params{}
	found, err := readGenesisField(source, "params", params)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.Params = params
	}

	tasks := []*oraclev1.OracleTask{}
	found, err = readGenesisField(source, "tasks", &tasks)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.Tasks = tasks
	}

	taskSchedule := []*oraclev1.OracleTaskScheduleEntry{}
	found, err = readGenesisField(source, "task_schedule", &taskSchedule)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.TaskSchedule = taskSchedule
	}

	latestValues := []*oraclev1.OracleValue{}
	found, err = readGenesisField(source, "latest_values", &latestValues)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.LatestValues = latestValues
	}

	history := []*oraclev1.OracleHistory{}
	found, err = readGenesisField(source, "history", &history)
	if err != nil {
		return nil, err
	}
	if found {
		genesis.History = history
	}

	return genesis, nil
}

func writeGenesisState(target appmodule.GenesisTarget, genesis *oraclev1.GenesisState) error {
	if genesis == nil {
		return oracletypes.ErrInvalidParams.Wrap("genesis state cannot be nil")
	}

	if err := writeGenesisField(target, "params", genesis.Params); err != nil {
		return err
	}
	if err := writeGenesisField(target, "tasks", genesis.Tasks); err != nil {
		return err
	}
	if err := writeGenesisField(target, "task_schedule", genesis.TaskSchedule); err != nil {
		return err
	}
	if err := writeGenesisField(target, "latest_values", genesis.LatestValues); err != nil {
		return err
	}

	return writeGenesisField(target, "history", genesis.History)
}

func readGenesisField(source appmodule.GenesisSource, fieldName string, value any) (bool, error) {
	reader, err := source(fieldName)
	if err != nil {
		return false, oracletypes.ErrReadGenesisField.Wrapf("%s: %v", fieldName, err)
	}
	if reader == nil {
		return false, nil
	}
	defer reader.Close()

	if err := json.NewDecoder(reader).Decode(value); err != nil {
		return false, oracletypes.ErrDecodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	return true, nil
}

func writeGenesisField(target appmodule.GenesisTarget, fieldName string, value any) error {
	writer, err := target(fieldName)
	if err != nil {
		return oracletypes.ErrOpenGenesisTargetField.Wrapf("%s: %v", fieldName, err)
	}
	if writer == nil {
		return oracletypes.ErrNilGenesisTargetWriter.Wrapf("%s genesis target field writer is nil", fieldName)
	}

	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return oracletypes.ErrEncodeGenesisField.Wrapf("%s: %v", fieldName, err)
	}

	if err := writer.Close(); err != nil {
		return oracletypes.ErrCloseGenesisFieldWriter.Wrapf("%s: %v", fieldName, err)
	}

	return nil
}
