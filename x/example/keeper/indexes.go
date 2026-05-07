package keeper

import (
	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/collections/indexes"

	exampletypes "github.com/gurufinglobal/guru/v3/x/example/types"
)

// projectIndexes는 Project 기본 맵에 연결되는 2차 인덱스 집합이다.
// collections.Indexes 인터페이스를 명시 구현하면
// reflection 추론 의존성을 줄여 유지보수성이 좋아진다.
type projectIndexes struct {
	Owner     *indexes.Multi[string, uint64, exampletypes.Project]
	OwnerName *indexes.Unique[collections.Pair[string, string], uint64, exampletypes.Project]
	Archived  *indexes.Multi[bool, uint64, exampletypes.Project]
}

func (i projectIndexes) IndexesList() []collections.Index[uint64, exampletypes.Project] {
	return []collections.Index[uint64, exampletypes.Project]{
		i.Owner,
		i.OwnerName,
		i.Archived,
	}
}

func newProjectIndexes(sb *collections.SchemaBuilder) projectIndexes {
	return projectIndexes{
		Owner: indexes.NewMulti(
			sb,
			exampletypes.ProjectsOwnerIndexKey,
			"projects_by_owner",
			collections.StringKey,
			collections.Uint64Key,
			func(_ uint64, p exampletypes.Project) (string, error) {
				return p.Owner, nil
			},
		),
		OwnerName: indexes.NewUnique(
			sb,
			exampletypes.ProjectsOwnerNameIndexKey,
			"projects_by_owner_name",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collections.Uint64Key,
			func(_ uint64, p exampletypes.Project) (collections.Pair[string, string], error) {
				return collections.Join(p.Owner, normalizeProjectName(p.Name)), nil
			},
		),
		Archived: indexes.NewMulti(
			sb,
			exampletypes.ProjectsArchivedIndexKey,
			"projects_by_archived",
			collections.BoolKey,
			collections.Uint64Key,
			func(_ uint64, p exampletypes.Project) (bool, error) {
				return p.Archived, nil
			},
		),
	}
}

// memberIndexes는 Pair(project_id, address) primary key를 가지는 멤버 맵을
// ReversePair 인덱스로 "address -> project_id" 방향으로 역조회할 수 있게 만든다.
type memberIndexes struct {
	ByAddress *indexes.ReversePair[uint64, string, exampletypes.ProjectMember]
}

func (i memberIndexes) IndexesList() []collections.Index[collections.Pair[uint64, string], exampletypes.ProjectMember] {
	return []collections.Index[collections.Pair[uint64, string], exampletypes.ProjectMember]{
		i.ByAddress,
	}
}

func newMemberIndexes(sb *collections.SchemaBuilder) memberIndexes {
	return memberIndexes{
		ByAddress: indexes.NewReversePair[exampletypes.ProjectMember](
			sb,
			exampletypes.ProjectMembersByAddressKey,
			"members_by_address",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
		),
	}
}

// taskIndexes는 Task 조회 성능을 위해 여러 축(project/assignee/status/due/external_ref)으로
// 인덱스를 동시에 유지한다.
type taskIndexes struct {
	ExternalRef   *indexes.Unique[string, uint64, exampletypes.Task]
	Project       *indexes.Multi[uint64, uint64, exampletypes.Task]
	Assignee      *indexes.Multi[string, uint64, exampletypes.Task]
	Status        *indexes.Multi[exampletypes.TaskStatus, uint64, exampletypes.Task]
	ProjectStatus *indexes.Multi[collections.Pair[uint64, exampletypes.TaskStatus], uint64, exampletypes.Task]
	DueAtNano     *indexes.Multi[int64, uint64, exampletypes.Task]
}

func (i taskIndexes) IndexesList() []collections.Index[uint64, exampletypes.Task] {
	return []collections.Index[uint64, exampletypes.Task]{
		i.ExternalRef,
		i.Project,
		i.Assignee,
		i.Status,
		i.ProjectStatus,
		i.DueAtNano,
	}
}

func newTaskIndexes(sb *collections.SchemaBuilder) taskIndexes {
	statusKeyCodec := collcodec.NewInt32Key[exampletypes.TaskStatus]()

	return taskIndexes{
		ExternalRef: indexes.NewUnique(
			sb,
			exampletypes.TasksExternalRefIndexKey,
			"tasks_by_external_ref",
			collections.StringKey,
			collections.Uint64Key,
			func(_ uint64, t exampletypes.Task) (string, error) {
				return normalizeExternalRef(t.ExternalRef), nil
			},
		),
		Project: indexes.NewMulti(
			sb,
			exampletypes.TasksByProjectIndexKey,
			"tasks_by_project",
			collections.Uint64Key,
			collections.Uint64Key,
			func(_ uint64, t exampletypes.Task) (uint64, error) {
				return t.ProjectId, nil
			},
		),
		Assignee: indexes.NewMulti(
			sb,
			exampletypes.TasksByAssigneeIndexKey,
			"tasks_by_assignee",
			collections.StringKey,
			collections.Uint64Key,
			func(_ uint64, t exampletypes.Task) (string, error) {
				return t.Assignee, nil
			},
		),
		Status: indexes.NewMulti(
			sb,
			exampletypes.TasksByStatusIndexKey,
			"tasks_by_status",
			statusKeyCodec,
			collections.Uint64Key,
			func(_ uint64, t exampletypes.Task) (exampletypes.TaskStatus, error) {
				return t.Status, nil
			},
		),
		ProjectStatus: indexes.NewMulti(
			sb,
			exampletypes.TasksByProjectStatusKey,
			"tasks_by_project_status",
			collections.PairKeyCodec(collections.Uint64Key, statusKeyCodec),
			collections.Uint64Key,
			func(_ uint64, t exampletypes.Task) (collections.Pair[uint64, exampletypes.TaskStatus], error) {
				return collections.Join(t.ProjectId, t.Status), nil
			},
		),
		DueAtNano: indexes.NewMulti(
			sb,
			exampletypes.TasksByDueAtIndexKey,
			"tasks_by_due_at_nano",
			collections.Int64Key,
			collections.Uint64Key,
			func(_ uint64, t exampletypes.Task) (int64, error) {
				return taskDueUnixNano(t), nil
			},
		),
	}
}
