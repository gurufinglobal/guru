package types

import (
	"fmt"
	"time"

	feemarkettypes "github.com/GPTx-global/guru-v2/x/feemarket/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
)

// 상수 정의
var (
	RegisterQuery       = "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'"
	RegisterID          = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId
	RegisterAccountList = oracletypes.EventTypeRegisterOracleRequestDoc + "." + oracletypes.AttributeKeyAccountList

	UpdateQuery = "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'"
	UpdateID    = oracletypes.EventTypeUpdateOracleRequestDoc + "." + oracletypes.AttributeKeyRequestId

	CompleteQuery = fmt.Sprintf("tm.event='NewBlock' AND %s.%s EXISTS", oracletypes.EventTypeCompleteOracleDataSet, oracletypes.AttributeKeyRequestId)
	CompleteID    = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyRequestId
	CompleteNonce = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyNonce
	CompleteTime  = oracletypes.EventTypeCompleteOracleDataSet + "." + oracletypes.AttributeKeyBlockTime

	MinGasPrice = feemarkettypes.EventTypeChangeMinGasPrice + "." + feemarkettypes.AttributeKeyMinGasPrice
)

// ========== 공통 전략 인터페이스 ==========

// BackoffStrategy는 재연결 시 지연 전략을 정의하는 인터페이스
type BackoffStrategy interface {
	// Next는 현재 시도 횟수에 따른 다음 지연 시간을 반환
	Next(attempt int) time.Duration

	// Reset은 백오프 전략을 초기 상태로 리셋
	Reset()
}

// RetryStrategy는 재시도 전략을 정의하는 인터페이스
type RetryStrategy interface {
	// ShouldRetry는 에러가 재시도 가능한지 확인
	ShouldRetry(err error, attempt int) bool

	// GetDelay는 재시도 간격을 반환
	GetDelay(attempt int) time.Duration

	// GetMaxRetries는 최대 재시도 횟수를 반환
	GetMaxRetries() int

	// Reset은 재시도 전략을 초기 상태로 리셋
	Reset()
}

// ========== 작업 상태 관련 ==========

// JobStatus는 작업 상태를 나타내는 열거형
type JobStatus int

const (
	JobStatusPending JobStatus = iota
	JobStatusScheduled
	JobStatusExecuting
	JobStatusCompleted
	JobStatusFailed
	JobStatusCancelled
	JobStatusRetrying
)

// String은 JobStatus의 문자열 표현을 반환
func (js JobStatus) String() string {
	switch js {
	case JobStatusPending:
		return "pending"
	case JobStatusScheduled:
		return "scheduled"
	case JobStatusExecuting:
		return "executing"
	case JobStatusCompleted:
		return "completed"
	case JobStatusFailed:
		return "failed"
	case JobStatusCancelled:
		return "cancelled"
	case JobStatusRetrying:
		return "retrying"
	default:
		return "unknown"
	}
}

// ExecutionState는 작업 실행 상태를 나타내는 구조체
type ExecutionState struct {
	Status        JobStatus  `json:"status"`
	StartedAt     time.Time  `json:"started_at,omitempty"`
	StartTime     *time.Time `json:"start_time,omitempty"` // 별칭 필드
	CompletedAt   time.Time  `json:"completed_at,omitempty"`
	EndTime       *time.Time `json:"end_time,omitempty"`       // 별칭 필드
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"` // 추가 필드
	Error         string     `json:"error,omitempty"`
	Progress      float64    `json:"progress"`
}

// ========== 헬스 상태 관련 ==========

// HealthStatus는 컴포넌트의 헬스 상태를 나타내는 문자열
type HealthStatus string

const (
	HealthStatusHealthy      HealthStatus = "healthy"
	HealthStatusDegraded     HealthStatus = "degraded"
	HealthStatusUnhealthy    HealthStatus = "unhealthy"
	HealthStatusInitializing HealthStatus = "initializing"
	HealthStatusStopping     HealthStatus = "stopping"
	HealthStatusStopped      HealthStatus = "stopped"
	HealthStatusFatal        HealthStatus = "fatal"
)

// ComponentHealth는 개별 컴포넌트의 헬스 상태를 나타내는 구조체
type ComponentHealth struct {
	Name        string        `json:"name"`
	Status      HealthStatus  `json:"status"`
	Message     string        `json:"message,omitempty"`
	LastChecked time.Time     `json:"last_checked"`
	Uptime      time.Duration `json:"uptime"`
}

// ========== 기본 메트릭 구조체 ==========

// BaseMetrics는 모든 컴포넌트 메트릭의 기본 구조체
type BaseMetrics struct {
	StartTime    time.Time     `json:"start_time"`
	StartedAt    time.Time     `json:"started_at"` // 별칭 필드
	Uptime       time.Duration `json:"uptime"`
	TotalEvents  int64         `json:"total_events"`
	TotalErrors  int64         `json:"total_errors"`
	LastEventAt  time.Time     `json:"last_event_at,omitempty"`
	LastErrorAt  time.Time     `json:"last_error_at,omitempty"`
	HealthStatus HealthStatus  `json:"health_status"`
}

// ========== 작업 관련 데이터 구조체 ==========

// OracleJob은 Oracle 작업을 나타내는 구조체 (통합된 버전)
type OracleJob struct {
	ID            string                    `json:"id"`
	RequestID     uint64                    `json:"request_id"`
	URL           string                    `json:"url"`
	ParseRule     string                    `json:"parse_rule"`
	Nonce         uint64                    `json:"nonce"`
	NextRunTime   time.Time                 `json:"next_run_time"`
	Period        time.Duration             `json:"period"`
	Status        oracletypes.RequestStatus `json:"status"`
	AccountList   []string                  `json:"account_list"`
	AssignedIndex int                       `json:"assigned_index"`

	// 메타데이터
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	RunCount     int64      `json:"run_count"`
	FailureCount int64      `json:"failure_count"`
	LastError    string     `json:"last_error,omitempty"`

	// 실행 상태
	ExecutionState ExecutionState `json:"execution_state"`
	Priority       int            `json:"priority"`
	RetryAttempts  int            `json:"retry_attempts"`
	MaxRetries     int            `json:"max_retries"`
}

// JobResult는 작업 실행 결과를 나타내는 구조체 (통합된 버전)
type JobResult struct {
	JobID      string        `json:"job_id"`
	RequestID  uint64        `json:"request_id"`
	Data       string        `json:"data"`
	Nonce      uint64        `json:"nonce"`
	Success    bool          `json:"success"`
	Error      error         `json:"error,omitempty"`
	ExecutedAt time.Time     `json:"executed_at"`
	Duration   time.Duration `json:"duration"`
}

// ========== 에러 관련 ==========

// ComponentError는 컴포넌트별 에러의 기본 인터페이스
type ComponentError interface {
	error
	Component() string
	IsRetryable() bool
	IsFatal() bool
}

// BaseError는 ComponentError의 기본 구현
type BaseError struct {
	ComponentName string    `json:"component"`
	Message       string    `json:"message"`
	Retryable     bool      `json:"retryable"`
	Fatal         bool      `json:"fatal"`
	Timestamp     time.Time `json:"timestamp"`
}

// Error는 error 인터페이스를 구현
func (e *BaseError) Error() string {
	return fmt.Sprintf("[%s] %s", e.ComponentName, e.Message)
}

// Component는 ComponentError 인터페이스를 구현
func (e *BaseError) Component() string {
	return e.ComponentName
}

// IsRetryable은 ComponentError 인터페이스를 구현
func (e *BaseError) IsRetryable() bool {
	return e.Retryable
}

// IsFatal은 ComponentError 인터페이스를 구현
func (e *BaseError) IsFatal() bool {
	return e.Fatal
}

// ========== 레거시 지원 (하위 호환성) ==========

// OracleJobResult는 하위 호환성을 위한 별칭
// Deprecated: JobResult를 사용하세요
type OracleJobResult = JobResult
