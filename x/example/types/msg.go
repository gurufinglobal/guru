package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// sdk.Msg는 v0.54에서 proto.Message alias이지만,
// 팀 데모 코드에서는 명시적으로 assertion을 둬서 "어떤 메시지가 트랜잭션 메시지인지"
// 한눈에 보이도록 유지한다.
var (
	_ sdk.Msg = (*MsgCreateProject)(nil)
	_ sdk.Msg = (*MsgUpdateProject)(nil)
	_ sdk.Msg = (*MsgAddProjectMember)(nil)
	_ sdk.Msg = (*MsgRemoveProjectMember)(nil)
	_ sdk.Msg = (*MsgCreateTask)(nil)
	_ sdk.Msg = (*MsgAssignTask)(nil)
	_ sdk.Msg = (*MsgUpdateTaskStatus)(nil)
	_ sdk.Msg = (*MsgAppendTaskEvent)(nil)
	_ sdk.Msg = (*MsgDeleteTask)(nil)
	_ sdk.Msg = (*MsgMigrateLegacyTaskValues)(nil)
	_ sdk.Msg = (*MsgUpdateParams)(nil)
)

// IsValid는 MemberRole enum 유효성을 점검한다.
func (r MemberRole) IsValid() bool {
	switch r {
	case MemberRole_MEMBER_ROLE_VIEWER, MemberRole_MEMBER_ROLE_EDITOR, MemberRole_MEMBER_ROLE_ADMIN:
		return true
	default:
		return false
	}
}

// IsValid는 TaskStatus enum 유효성을 점검한다.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatus_TASK_STATUS_TODO,
		TaskStatus_TASK_STATUS_IN_PROGRESS,
		TaskStatus_TASK_STATUS_BLOCKED,
		TaskStatus_TASK_STATUS_DONE,
		TaskStatus_TASK_STATUS_ARCHIVED:
		return true
	default:
		return false
	}
}

// IsOpenTaskStatus는 "열린 작업(open task)" 여부를 판단한다.
// 통계/알림/담당자 큐 계산에서 동일 기준을 공유하기 위해 types 계층에 둔다.
func IsOpenTaskStatus(s TaskStatus) bool {
	switch s {
	case TaskStatus_TASK_STATUS_TODO,
		TaskStatus_TASK_STATUS_IN_PROGRESS,
		TaskStatus_TASK_STATUS_BLOCKED:
		return true
	default:
		return false
	}
}

func (m *MsgCreateProject) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Creator); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("%w: project name is empty", ErrInvalidRequest)
	}
	if err := ValidateMaxLen("name", m.Name, MaxProjectNameLength); err != nil {
		return err
	}
	if err := ValidateMaxLen("description", m.Description, MaxProjectDescriptionLength); err != nil {
		return err
	}
	if err := ValidateMaxLen("metadata_json", m.MetadataJson, MaxMetadataJSONLength); err != nil {
		return err
	}
	return nil
}

func (m *MsgUpdateProject) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Editor); err != nil {
		return err
	}
	if err := validatePositiveID("project_id", m.ProjectId); err != nil {
		return err
	}
	if m.Name != "" {
		if err := ValidateMaxLen("name", m.Name, MaxProjectNameLength); err != nil {
			return err
		}
	}
	if err := ValidateMaxLen("description", m.Description, MaxProjectDescriptionLength); err != nil {
		return err
	}
	if err := ValidateMaxLen("metadata_json", m.MetadataJson, MaxMetadataJSONLength); err != nil {
		return err
	}
	return nil
}

func (m *MsgAddProjectMember) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Actor); err != nil {
		return err
	}
	if err := ValidateAddress(m.Member); err != nil {
		return err
	}
	if err := validatePositiveID("project_id", m.ProjectId); err != nil {
		return err
	}
	if !m.Role.IsValid() {
		return fmt.Errorf("%w: invalid member role", ErrInvalidRole)
	}
	return nil
}

func (m *MsgRemoveProjectMember) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Actor); err != nil {
		return err
	}
	if err := ValidateAddress(m.Member); err != nil {
		return err
	}
	if err := validatePositiveID("project_id", m.ProjectId); err != nil {
		return err
	}
	return nil
}

func (m *MsgCreateTask) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Creator); err != nil {
		return err
	}
	if err := ValidateOptionalAddress(m.Assignee); err != nil {
		return err
	}
	if err := validatePositiveID("project_id", m.ProjectId); err != nil {
		return err
	}
	if strings.TrimSpace(m.ExternalRef) == "" {
		return fmt.Errorf("%w: external_ref is empty", ErrInvalidRequest)
	}
	if strings.TrimSpace(m.Title) == "" {
		return fmt.Errorf("%w: title is empty", ErrInvalidRequest)
	}
	if err := ValidateMaxLen("title", m.Title, MaxTaskTitleLength); err != nil {
		return err
	}
	if err := ValidateMaxLen("description", m.Description, MaxTaskDescriptionLength); err != nil {
		return err
	}
	if err := ValidateMaxLen("metadata_json", m.MetadataJson, MaxMetadataJSONLength); err != nil {
		return err
	}
	if err := validateLabels(m.Labels); err != nil {
		return err
	}
	return nil
}

func (m *MsgAssignTask) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Operator); err != nil {
		return err
	}
	if err := ValidateOptionalAddress(m.Assignee); err != nil {
		return err
	}
	if err := validatePositiveID("task_id", m.TaskId); err != nil {
		return err
	}
	return nil
}

func (m *MsgUpdateTaskStatus) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Operator); err != nil {
		return err
	}
	if err := validatePositiveID("task_id", m.TaskId); err != nil {
		return err
	}
	if !m.Status.IsValid() {
		return fmt.Errorf("%w: invalid status", ErrInvalidStatus)
	}
	if err := ValidateMaxLen("reason", m.Reason, MaxTaskDescriptionLength); err != nil {
		return err
	}
	return nil
}

func (m *MsgAppendTaskEvent) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Actor); err != nil {
		return err
	}
	if err := validatePositiveID("task_id", m.TaskId); err != nil {
		return err
	}
	if strings.TrimSpace(m.EventType) == "" {
		return fmt.Errorf("%w: event_type is empty", ErrInvalidRequest)
	}
	if err := ValidateMaxLen("event_type", m.EventType, MaxEventTypeLength); err != nil {
		return err
	}
	if err := ValidateMaxLen("payload_json", m.PayloadJson, MaxEventPayloadLength); err != nil {
		return err
	}
	return nil
}

func (m *MsgDeleteTask) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Operator); err != nil {
		return err
	}
	if err := validatePositiveID("task_id", m.TaskId); err != nil {
		return err
	}
	return nil
}

func (m *MsgMigrateLegacyTaskValues) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Authority); err != nil {
		return err
	}
	return nil
}

func (m *MsgUpdateParams) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalidRequest)
	}
	if err := ValidateAddress(m.Authority); err != nil {
		return err
	}
	return m.Params.Validate()
}

func validatePositiveID(field string, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: %s must be > 0", ErrInvalidRequest, field)
	}
	return nil
}

func validateLabels(labels []string) error {
	if len(labels) > MaxLabelCount {
		return fmt.Errorf("%w: too many labels (max=%d)", ErrInvalidRequest, MaxLabelCount)
	}
	for _, label := range labels {
		if err := ValidateMaxLen("label", label, MaxLabelLength); err != nil {
			return err
		}
	}
	return nil
}
