package types

import errorsmod "cosmossdk.io/errors"

// 모듈 에러 코드는 체인 합의 상태와 직접 연결되므로,
// 한 번 배포된 뒤에는 재사용/의미 변경을 피하는 것이 중요하다.
// 데모 모듈이지만 "실전처럼" 코드를 분리해 팀에 패턴을 보여주기 위해 명시적으로 정의한다.
var (
	ErrInvalidAuthority       = errorsmod.Register(ModuleName, 1, "invalid authority")
	ErrInvalidRequest         = errorsmod.Register(ModuleName, 2, "invalid request")
	ErrProjectNotFound        = errorsmod.Register(ModuleName, 3, "project not found")
	ErrTaskNotFound           = errorsmod.Register(ModuleName, 4, "task not found")
	ErrTaskEventNotFound      = errorsmod.Register(ModuleName, 5, "task event not found")
	ErrPermissionDenied       = errorsmod.Register(ModuleName, 6, "permission denied")
	ErrLimitExceeded          = errorsmod.Register(ModuleName, 7, "limit exceeded")
	ErrConflict               = errorsmod.Register(ModuleName, 8, "state conflict")
	ErrInvalidRole            = errorsmod.Register(ModuleName, 9, "invalid role")
	ErrInvalidStatus          = errorsmod.Register(ModuleName, 10, "invalid status")
	ErrMemberNotFound         = errorsmod.Register(ModuleName, 11, "member not found")
	ErrOwnerRemovalForbidden  = errorsmod.Register(ModuleName, 12, "owner cannot be removed")
	ErrInvalidPaginationKey   = errorsmod.Register(ModuleName, 13, "invalid pagination key")
	ErrLegacyMigrationFailure = errorsmod.Register(ModuleName, 14, "legacy migration failure")
)
