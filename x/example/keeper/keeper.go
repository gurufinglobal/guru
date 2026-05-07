package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	exampletypes "github.com/gurufinglobal/guru/v3/x/example/types"
)

// Keeper는 x/example 모듈의 상태 접근 계층이다.
//
// 이 Keeper는 데모 목적상 collections의 주요 기능을 모두 담는다:
// - Item / Map / KeySet / Sequence / IndexedMap
// - indexes.Unique / indexes.Multi / indexes.ReversePair
// - Pair / Triple / Quad composite key
// - LookupMap / Vec
// - CollInterfaceValue / AltValueCodec
type Keeper struct {
	cdc          sdkcodec.BinaryCodec
	storeService store.KVStoreService
	authority    string

	// Schema는 컬렉션 구조를 introspection/시뮬레이션/디버깅에 재사용할 수 있도록 보관한다.
	Schema collections.Schema

	// ---- Core state ----
	ParamsItem        collections.Item[exampletypes.Params]
	ProjectSequence   collections.Sequence
	TaskSequence      collections.Sequence
	TaskEventSequence collections.Sequence

	// ---- Main domain maps ----
	ProjectStore   *collections.IndexedMap[uint64, exampletypes.Project, projectIndexes]
	Members        *collections.IndexedMap[collections.Pair[uint64, string], exampletypes.ProjectMember, memberIndexes]
	Tasks          *collections.IndexedMap[uint64, exampletypes.Task, taskIndexes]
	TaskEventStore collections.Map[uint64, exampletypes.TaskEvent]

	// ---- Auxiliary relations / indexes by explicit composite keys ----
	ProjectAdmins             collections.KeySet[collections.Pair[uint64, string]]
	TaskEventsByTask          collections.KeySet[collections.Pair[uint64, uint64]]
	TaskEventsByProjectTask   collections.KeySet[collections.Triple[uint64, uint64, uint64]]
	TaskLabelsByProjectStatus collections.KeySet[collections.Quad[uint64, string, int32, uint64]]

	// ---- Aggregates ----
	ProjectCount     collections.Item[uint64]
	TaskCount        collections.Item[uint64]
	TaskEventCount   collections.Item[uint64]
	TaskStatusCounts collections.Map[exampletypes.TaskStatus, uint64]

	// ---- Advanced collection examples ----
	AssigneeOpenTaskCounts collections.LookupMap[string, uint64]
	EventMessageByEventID  collections.Map[uint64, sdk.Msg]
	LegacyTaskPriority     collections.Map[uint64, uint32]
	RecentTaskIDs          collections.Vec[uint64]
}

// NewKeeper는 x/example keeper를 구성한다.
func NewKeeper(
	cdc sdkcodec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
) Keeper {
	if authority == "" {
		panic("x/example authority must not be empty")
	}

	sb := collections.NewSchemaBuilder(storeService)

	statusKeyCodec := collcodec.NewInt32Key[exampletypes.TaskStatus]()

	k := Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,

		ParamsItem:        collections.NewItem(sb, exampletypes.ParamsKey, "params", sdkcodec.CollValue[exampletypes.Params](cdc)),
		ProjectSequence:   collections.NewSequence(sb, exampletypes.ProjectSequenceKey, "project_sequence"),
		TaskSequence:      collections.NewSequence(sb, exampletypes.TaskSequenceKey, "task_sequence"),
		TaskEventSequence: collections.NewSequence(sb, exampletypes.TaskEventSequenceKey, "task_event_sequence"),

		ProjectStore: collections.NewIndexedMap(
			sb,
			exampletypes.ProjectsKey,
			"projects",
			collections.Uint64Key,
			sdkcodec.CollValue[exampletypes.Project](cdc),
			newProjectIndexes(sb),
		),
		Members: collections.NewIndexedMap(
			sb,
			exampletypes.ProjectMembersKey,
			"project_members",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			sdkcodec.CollValue[exampletypes.ProjectMember](cdc),
			newMemberIndexes(sb),
		),
		Tasks: collections.NewIndexedMap(
			sb,
			exampletypes.TasksKey,
			"tasks",
			collections.Uint64Key,
			sdkcodec.CollValue[exampletypes.Task](cdc),
			newTaskIndexes(sb),
		),
		TaskEventStore: collections.NewMap(
			sb,
			exampletypes.TaskEventsKey,
			"task_events",
			collections.Uint64Key,
			sdkcodec.CollValue[exampletypes.TaskEvent](cdc),
		),

		ProjectAdmins: collections.NewKeySet(
			sb,
			exampletypes.ProjectAdminsKey,
			"project_admins",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
		),
		TaskEventsByTask: collections.NewKeySet(
			sb,
			exampletypes.TaskEventsByTaskKey,
			"task_events_by_task",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key),
		),
		TaskEventsByProjectTask: collections.NewKeySet(
			sb,
			exampletypes.TaskEventsByProjectTaskKey,
			"task_events_by_project_task",
			collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key),
		),
		TaskLabelsByProjectStatus: collections.NewKeySet(
			sb,
			exampletypes.TaskLabelsByProjectStatusKey,
			"task_labels_by_project_status",
			collections.QuadKeyCodec(collections.Uint64Key, collections.StringKey, collections.Int32Key, collections.Uint64Key),
		),

		ProjectCount: collections.NewItem(sb, exampletypes.ProjectCountKey, "project_count", collections.Uint64Value),
		TaskCount:    collections.NewItem(sb, exampletypes.TaskCountKey, "task_count", collections.Uint64Value),
		TaskEventCount: collections.NewItem(
			sb,
			exampletypes.TaskEventCountKey,
			"task_event_count",
			collections.Uint64Value,
		),
		TaskStatusCounts: collections.NewMap(
			sb,
			exampletypes.TaskStatusCountKey,
			"task_status_count",
			statusKeyCodec,
			collections.Uint64Value,
		),

		AssigneeOpenTaskCounts: collections.NewLookupMap(
			sb,
			exampletypes.AssigneeOpenTaskCountKey,
			"assignee_open_task_count",
			collections.StringKey,
			collections.Uint64Value,
		),
		EventMessageByEventID: collections.NewMap(
			sb,
			exampletypes.EventMessageByEventIDKey,
			"event_message_by_event_id",
			collections.Uint64Key,
			sdkcodec.CollInterfaceValue[sdk.Msg](cdc),
		),
		LegacyTaskPriority: collections.NewMap(
			sb,
			exampletypes.LegacyTaskPriorityKey,
			"legacy_task_priority",
			collections.Uint64Key,
			exampletypes.LegacyTaskPriorityValueCodec,
		),
		RecentTaskIDs: collections.NewVec(
			sb,
			exampletypes.RecentTaskIDsKey,
			"recent_task_ids",
			collections.Uint64Value,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("x/example schema build failed: %w", err))
	}
	k.Schema = schema

	return k
}

// GetAuthority는 파라미터 변경/마이그레이션 같은 privileged 메시지의 인증 기준이다.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger는 x/example prefix를 고정해 로그 검색성을 높인다.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+exampletypes.ModuleName)
}
