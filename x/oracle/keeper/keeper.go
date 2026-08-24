package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

type Keeper struct {
	accountCodec       address.Codec
	constitutionKeeper ConstitutionKeeper
	hooks              oracletypes.OracleHooks

	params               collections.Item[oracletypes.Params]
	tasks                collections.Map[string, oracletypes.OracleTask]
	latest               collections.Map[string, oracletypes.OracleValue]
	history              collections.Map[string, oracletypes.OracleHistory]
	taskSchedule         collections.KeySet[collections.Pair[int64, string]]
	taskScheduleBySymbol collections.KeySet[collections.Pair[string, int64]]

	schema collections.Schema
}

func NewKeeper(
	storeService store.KVStoreService,
	cdc codec.Codec,
	accountCodec address.Codec,
	constitutionKeeper ConstitutionKeeper,
) Keeper {
	k := Keeper{
		accountCodec:       accountCodec,
		constitutionKeeper: constitutionKeeper,
	}

	sb := collections.NewSchemaBuilder(storeService)
	k.params = collections.NewItem(sb, oracletypes.ParamsKey, "params", codec.CollValue[oracletypes.Params](cdc))
	k.tasks = collections.NewMap(sb, oracletypes.TasksKey, "tasks", collections.StringKey, codec.CollValue[oracletypes.OracleTask](cdc))
	k.latest = collections.NewMap(sb, oracletypes.LatestKey, "latest", collections.StringKey, codec.CollValue[oracletypes.OracleValue](cdc))
	k.history = collections.NewMap(sb, oracletypes.HistoryKey, "history", collections.StringKey, codec.CollValue[oracletypes.OracleHistory](cdc))
	k.taskSchedule = collections.NewKeySet(
		sb,
		oracletypes.TaskScheduleKey,
		"task_schedule",
		collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
	)
	k.taskScheduleBySymbol = collections.NewKeySet(
		sb,
		oracletypes.TaskScheduleBySymbolKey,
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

func (k *Keeper) SetHooks(hooks oracletypes.OracleHooks) {
	k.hooks = hooks
}

func (k Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+oracletypes.ModuleName)
}
