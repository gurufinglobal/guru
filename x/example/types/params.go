package types

import "fmt"

const (
	DefaultMaxProjectsPerOwner  uint32 = 32
	DefaultMaxMembersPerProject uint32 = 64
	DefaultMaxTasksPerProject   uint32 = 2000
)

// DefaultParams는 신규 체인 기준의 기본 파라미터를 반환한다.
// 데모 모듈이더라도 "파라미터 기반 운영" 패턴을 팀에 보여주기 위해
// 하드코딩 대신 명시적 Params 상태를 사용한다.
func DefaultParams() Params {
	return Params{
		MaxProjectsPerOwner:  DefaultMaxProjectsPerOwner,
		MaxMembersPerProject: DefaultMaxMembersPerProject,
		MaxTasksPerProject:   DefaultMaxTasksPerProject,
	}
}

// Validate는 파라미터 기본 무결성을 검증한다.
func (p Params) Validate() error {
	if p.MaxProjectsPerOwner == 0 {
		return fmt.Errorf("%w: max_projects_per_owner must be > 0", ErrInvalidRequest)
	}
	if p.MaxMembersPerProject == 0 {
		return fmt.Errorf("%w: max_members_per_project must be > 0", ErrInvalidRequest)
	}
	if p.MaxTasksPerProject == 0 {
		return fmt.Errorf("%w: max_tasks_per_project must be > 0", ErrInvalidRequest)
	}

	// 비정상적으로 큰 값은 데모 환경에서도 실수 입력일 가능성이 높아 방어한다.
	if p.MaxProjectsPerOwner > 1_000_000 {
		return fmt.Errorf("%w: max_projects_per_owner is too large", ErrInvalidRequest)
	}
	if p.MaxMembersPerProject > 1_000_000 {
		return fmt.Errorf("%w: max_members_per_project is too large", ErrInvalidRequest)
	}
	if p.MaxTasksPerProject > 10_000_000 {
		return fmt.Errorf("%w: max_tasks_per_project is too large", ErrInvalidRequest)
	}

	return nil
}
