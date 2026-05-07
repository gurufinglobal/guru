package types

import (
	"fmt"
	"strings"
)

// DefaultGenesisState는 모듈 기본 제네시스 상태를 반환한다.
// 시퀀스를 1부터 시작하면 사람이 읽는 ID(1,2,3...)와 직관이 맞아
// 데모/운영 로그 분석이 쉬워진다.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:          DefaultParams(),
		NextProjectId:   1,
		NextTaskId:      1,
		NextTaskEventId: 1,
		Projects:        []Project{},
		Members:         []ProjectMember{},
		Tasks:           []Task{},
		TaskEvents:      []TaskEvent{},
	}
}

// ValidateGenesis는 제네시스 데이터 무결성을 검증한다.
func ValidateGenesis(state GenesisState) error {
	if err := state.Params.Validate(); err != nil {
		return err
	}

	projectIDs := make(map[uint64]struct{}, len(state.Projects))
	for _, p := range state.Projects {
		if p.Id == 0 {
			return fmt.Errorf("%w: project id must be > 0", ErrInvalidRequest)
		}
		if strings.TrimSpace(p.Owner) == "" {
			return fmt.Errorf("%w: project owner is empty", ErrInvalidRequest)
		}
		if _, exists := projectIDs[p.Id]; exists {
			return fmt.Errorf("%w: duplicated project id %d", ErrConflict, p.Id)
		}
		projectIDs[p.Id] = struct{}{}
	}

	memberKeys := make(map[string]struct{}, len(state.Members))
	for _, m := range state.Members {
		if m.ProjectId == 0 {
			return fmt.Errorf("%w: member project id must be > 0", ErrInvalidRequest)
		}
		key := fmt.Sprintf("%d/%s", m.ProjectId, m.Address)
		if _, exists := memberKeys[key]; exists {
			return fmt.Errorf("%w: duplicated project member %s", ErrConflict, key)
		}
		memberKeys[key] = struct{}{}
	}

	taskIDs := make(map[uint64]struct{}, len(state.Tasks))
	for _, t := range state.Tasks {
		if t.Id == 0 {
			return fmt.Errorf("%w: task id must be > 0", ErrInvalidRequest)
		}
		if _, exists := taskIDs[t.Id]; exists {
			return fmt.Errorf("%w: duplicated task id %d", ErrConflict, t.Id)
		}
		taskIDs[t.Id] = struct{}{}
	}

	taskEventIDs := make(map[uint64]struct{}, len(state.TaskEvents))
	for _, e := range state.TaskEvents {
		if e.Id == 0 {
			return fmt.Errorf("%w: task event id must be > 0", ErrInvalidRequest)
		}
		if _, exists := taskEventIDs[e.Id]; exists {
			return fmt.Errorf("%w: duplicated task event id %d", ErrConflict, e.Id)
		}
		taskEventIDs[e.Id] = struct{}{}
	}

	return nil
}
