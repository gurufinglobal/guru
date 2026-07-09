package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

type Keeper struct {
	accountCodec       address.Codec
	constitutionKeeper ConstitutionKeeper
	hooks              types.OracleHooks

	params               collections.Item[*oraclev1.Params]
	tasks                collections.Map[string, *oraclev1.OracleTask]
	latest               collections.Map[string, *oraclev1.OracleValue]
	history              collections.Map[string, *oraclev1.OracleHistory]
	taskSchedule         collections.KeySet[collections.Pair[int64, string]]
	taskScheduleBySymbol collections.KeySet[collections.Pair[string, int64]]

	schema collections.Schema
}

func NewKeeper(
	storeService store.KVStoreService,
	accountCodec address.Codec,
	constitutionKeeper ConstitutionKeeper,
) Keeper {
	k := Keeper{
		accountCodec:       accountCodec,
		constitutionKeeper: constitutionKeeper,
	}

	sb := collections.NewSchemaBuilder(storeService)
	k.params = collections.NewItem(sb, types.ParamsKey, "params", codec.CollValueV2[oraclev1.Params]())
	k.tasks = collections.NewMap(sb, types.TasksKey, "tasks", collections.StringKey, codec.CollValueV2[oraclev1.OracleTask]())
	k.latest = collections.NewMap(sb, types.LatestKey, "latest", collections.StringKey, codec.CollValueV2[oraclev1.OracleValue]())
	k.history = collections.NewMap(sb, types.HistoryKey, "history", collections.StringKey, codec.CollValueV2[oraclev1.OracleHistory]())
	k.taskSchedule = collections.NewKeySet(
		sb,
		types.TaskScheduleKey,
		"task_schedule",
		collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
	)
	k.taskScheduleBySymbol = collections.NewKeySet(
		sb,
		types.TaskScheduleBySymbolKey,
		"task_schedule_by_symbol",
		collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
	)

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.schema = schema

	return k
}

func (k *Keeper) SetHooks(hooks types.OracleHooks) {
	k.hooks = hooks
}

func (k Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
}
