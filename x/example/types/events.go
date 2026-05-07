package types

// 이벤트 이름/속성 키를 상수로 통일하면
// - 인덱서/모니터링 파이프라인에서 문자열 오타를 줄일 수 있고
// - 팀 내 이벤트 계약(contract)을 문서화하기 쉬워진다.
const (
	EventTypeCreateProject       = "example.create_project"
	EventTypeUpdateProject       = "example.update_project"
	EventTypeAddProjectMember    = "example.add_project_member"
	EventTypeRemoveProjectMember = "example.remove_project_member"
	EventTypeCreateTask          = "example.create_task"
	EventTypeAssignTask          = "example.assign_task"
	EventTypeUpdateTaskStatus    = "example.update_task_status"
	EventTypeAppendTaskEvent     = "example.append_task_event"
	EventTypeDeleteTask          = "example.delete_task"
	EventTypeMigrateLegacy       = "example.migrate_legacy"
	EventTypeUpdateParams        = "example.update_params"
)

const (
	AttributeKeyProjectID   = "project_id"
	AttributeKeyTaskID      = "task_id"
	AttributeKeyTaskEventID = "task_event_id"
	AttributeKeyActor       = "actor"
	AttributeKeyOwner       = "owner"
	AttributeKeyMember      = "member"
	AttributeKeyAssignee    = "assignee"
	AttributeKeyStatus      = "status"
	AttributeKeyRole        = "role"
	AttributeKeyReason      = "reason"
	AttributeKeyMigrated    = "migrated_count"
)
