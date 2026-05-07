package types

import "cosmossdk.io/collections"

const (
	// ModuleName은 모듈의 고유 이름이다.
	// app wiring 시 module manager, genesis map, 권한 문자열 등에 동일하게 사용된다.
	ModuleName = "example"

	// StoreKey는 모듈 KV 스토어 키이다.
	// 현재 체인은 신규 체인이므로 레거시 키 호환 계층 없이 단일 키를 사용한다.
	StoreKey = ModuleName

	// RouterKey는 Msg 라우팅 키이다.
	// SDK v0.54에서는 protobuf MsgService가 기본 경로지만, 관례상 상수는 유지한다.
	RouterKey = ModuleName
)

// 컬렉션 prefix 설계 원칙:
// 1) 사람이 읽기 쉬운 이름(name)과 prefix를 1:1로 고정한다.
// 2) prefix 중첩(overlap)을 피하기 위해 기능군 단위로 블록을 나눈다.
// 3) Vec는 내부적으로 prefix에 suffix를 덧붙여 2개 컬렉션을 생성하므로 별도 영역을 확보한다.
var (
	// ---- Core params / sequence ----
	ParamsKey            = collections.NewPrefix(0)
	ProjectSequenceKey   = collections.NewPrefix(1)
	TaskSequenceKey      = collections.NewPrefix(2)
	TaskEventSequenceKey = collections.NewPrefix(3)

	// ---- Project domain ----
	ProjectsKey                = collections.NewPrefix(10)
	ProjectsOwnerIndexKey      = collections.NewPrefix(11)
	ProjectsOwnerNameIndexKey  = collections.NewPrefix(12)
	ProjectsArchivedIndexKey   = collections.NewPrefix(13)
	ProjectMembersKey          = collections.NewPrefix(20)
	ProjectMembersByAddressKey = collections.NewPrefix(21)
	ProjectAdminsKey           = collections.NewPrefix(22)

	// ---- Task domain ----
	TasksKey                 = collections.NewPrefix(30)
	TasksExternalRefIndexKey = collections.NewPrefix(31)
	TasksByProjectIndexKey   = collections.NewPrefix(32)
	TasksByAssigneeIndexKey  = collections.NewPrefix(33)
	TasksByStatusIndexKey    = collections.NewPrefix(34)
	TasksByProjectStatusKey  = collections.NewPrefix(35)
	TasksByDueAtIndexKey     = collections.NewPrefix(36)

	// ---- Event / advanced composite key domain ----
	TaskEventsKey                = collections.NewPrefix(40)
	TaskEventsByTaskKey          = collections.NewPrefix(41)
	TaskEventsByProjectTaskKey   = collections.NewPrefix(42)
	TaskLabelsByProjectStatusKey = collections.NewPrefix(43)

	// ---- Aggregates / counters ----
	ProjectCountKey    = collections.NewPrefix(50)
	TaskCountKey       = collections.NewPrefix(51)
	TaskEventCountKey  = collections.NewPrefix(52)
	TaskStatusCountKey = collections.NewPrefix(53)

	// ---- Advanced examples ----
	AssigneeOpenTaskCountKey = collections.NewPrefix(60) // LookupMap
	EventMessageByEventIDKey = collections.NewPrefix(70) // CollInterfaceValue
	LegacyTaskPriorityKey    = collections.NewPrefix(80) // AltValueCodec
	RecentTaskIDsKey         = collections.NewPrefix(90) // Vec (prefix+suffix 두 개 컬렉션 생성)
)
