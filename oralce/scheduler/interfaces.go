package scheduler

import (
	"context"
	"time"

	commontypes "github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// EventScheduler는 이벤트를 작업으로 변환하고 스케줄링하는 메인 인터페이스
type EventScheduler interface {
	// Start는 스케줄러를 시작하고 기존 작업들을 로드
	Start(ctx context.Context) error

	// Stop은 모든 작업을 중지하고 리소스를 정리
	Stop(ctx context.Context) error

	// ProcessEvent는 블록체인 이벤트를 처리하여 작업으로 변환
	ProcessEvent(ctx context.Context, event coretypes.ResultEvent) error

	// GetResultChannel는 완료된 작업 결과를 전달하는 채널 반환
	GetResultChannel() <-chan *JobResult

	// GetMetrics는 현재 스케줄러 메트릭을 반환
	GetMetrics() *SchedulerMetrics

	// GetJobStatus는 특정 작업의 상태를 반환
	GetJobStatus(jobID string) (JobStatus, bool)

	// IsRunning은 스케줄러 실행 상태를 반환
	IsRunning() bool
}

// JobStore는 작업 저장소 인터페이스
type JobStore interface {
	// Store는 작업을 저장 (key, job)
	Store(jobID string, job *OracleJob) error

	// Get은 작업 ID로 작업을 조회
	Get(jobID string) (*OracleJob, bool)

	// Update는 기존 작업을 업데이트
	Update(jobID string, updater func(*OracleJob) error) error

	// Delete는 작업을 삭제
	Delete(jobID string) error

	// List는 모든 작업을 나열
	List() []*OracleJob

	// ListByStatus는 특정 상태의 작업들을 나열
	ListByStatus(status JobStatus) []*OracleJob

	// ListReadyJobs는 실행 준비된 작업들을 나열
	ListReadyJobs(now time.Time) []*OracleJob

	// GetReadyJobs는 실행 준비된 작업들을 반환 (alias)
	GetReadyJobs(now time.Time) []*OracleJob

	// Count는 총 작업 수를 반환
	Count() int
}

// JobExecutor는 작업 실행을 담당하는 인터페이스
type JobExecutor interface {
	// Execute는 단일 작업을 실행
	Execute(ctx context.Context, job *OracleJob) (*JobResult, error)

	// ExecuteAsync는 작업을 비동기로 실행
	ExecuteAsync(ctx context.Context, job *OracleJob, resultCh chan<- *JobResult) error

	// SubmitTask는 임의의 태스크를 비동기로 실행
	SubmitTask(task func() error)

	// FetchData는 외부 URL에서 데이터를 가져옴
	FetchData(ctx context.Context, url string) ([]byte, error)

	// ParseAndExtract는 JSON 데이터를 파싱하고 경로에 따라 값을 추출
	ParseAndExtract(rawData []byte, parseRule string) (string, error)

	// GetCapacity는 현재 실행 가능한 작업 수를 반환
	GetCapacity() int

	// GetActiveJobs는 현재 실행 중인 작업 수를 반환
	GetActiveJobs() int

	// Shutdown은 실행기를 종료
	Shutdown(ctx context.Context) error
}

// EventParser는 블록체인 이벤트를 파싱하는 인터페이스
type EventParser interface {
	// ParseRegisterEvent는 등록 이벤트를 파싱
	ParseRegisterEvent(ctx context.Context, event coretypes.ResultEvent) (*oracletypes.OracleRequestDoc, error)

	// ParseUpdateEvent는 업데이트 이벤트를 파싱
	ParseUpdateEvent(ctx context.Context, event coretypes.ResultEvent) (*oracletypes.OracleRequestDoc, error)

	// ParseCompleteEvent는 완료 이벤트를 파싱
	ParseCompleteEvent(event coretypes.ResultEvent) (*CompleteEventData, error)

	// IsEventSupported는 이벤트가 지원되는지 확인
	IsEventSupported(event coretypes.ResultEvent) bool

	// ConvertToJob는 요청 문서를 Oracle 작업으로 변환
	ConvertToJob(doc *oracletypes.OracleRequestDoc, assignedAddress string, blockTime time.Time) (*OracleJob, error)
}

// QueryClient는 블록체인 쿼리를 위한 인터페이스
type QueryClient interface {
	// OracleRequestDoc은 특정 요청 문서를 조회
	OracleRequestDoc(ctx context.Context, req *oracletypes.QueryOracleRequestDocRequest) (*oracletypes.QueryOracleRequestDocResponse, error)

	// OracleRequestDocs는 요청 문서 목록을 조회
	OracleRequestDocs(ctx context.Context, req *oracletypes.QueryOracleRequestDocsRequest) (*oracletypes.QueryOracleRequestDocsResponse, error)

	// OracleData는 Oracle 데이터를 조회
	OracleData(ctx context.Context, req *oracletypes.QueryOracleDataRequest) (*oracletypes.QueryOracleDataResponse, error)
}

// WorkerPool는 워커 풀 관리 인터페이스
type WorkerPool interface {
	// SubmitJob은 작업을 워커 풀에 제출
	SubmitJob(ctx context.Context, job func() error) error

	// GetStats는 워커 풀 통계를 반환
	GetStats() WorkerPoolStats

	// Resize는 워커 풀 크기를 변경
	Resize(newSize int) error

	// Shutdown은 워커 풀을 종료
	Shutdown(ctx context.Context) error
}

// SchedulingStrategy는 작업 스케줄링 전략을 정의하는 인터페이스
type SchedulingStrategy interface {
	// CalculateNextRun은 다음 실행 시간을 계산
	CalculateNextRun(job *OracleJob, lastRun time.Time) time.Time

	// ShouldExecuteNow는 지금 실행해야 하는지 결정
	ShouldExecuteNow(job *OracleJob, now time.Time) bool

	// GetPriority는 작업의 우선순위를 반환
	GetPriority(job *OracleJob) int
}

// 데이터 구조체들

// OracleJob는 commontypes.OracleJob의 별칭
// Deprecated: commontypes.OracleJob을 직접 사용하세요
type OracleJob = commontypes.OracleJob

// JobResult는 commontypes.JobResult의 별칭
// Deprecated: commontypes.JobResult를 직접 사용하세요
type JobResult = commontypes.JobResult

// JobStatus는 commontypes.JobStatus의 별칭
// Deprecated: commontypes.JobStatus를 직접 사용하세요
type JobStatus = commontypes.JobStatus

const (
	JobStatusPending   = commontypes.JobStatusPending
	JobStatusScheduled = commontypes.JobStatusScheduled
	JobStatusExecuting = commontypes.JobStatusExecuting
	JobStatusCompleted = commontypes.JobStatusCompleted
	JobStatusFailed    = commontypes.JobStatusFailed
	JobStatusCancelled = commontypes.JobStatusCancelled
	JobStatusRetrying  = commontypes.JobStatusRetrying
)

// String은 JobStatus의 문자열 표현을 반환
// Deprecated: commontypes.JobStatus.String()을 직접 사용하세요
func JobStatusString(s JobStatus) string {
	return commontypes.JobStatus(s).String()
}

// ExecutionState는 작업 실행 상태를 나타내는 구조체
type ExecutionState struct {
	Status        JobStatus  `json:"status"`
	StartTime     *time.Time `json:"start_time,omitempty"`
	EndTime       *time.Time `json:"end_time,omitempty"`
	WorkerID      string     `json:"worker_id,omitempty"`
	LastHeartbeat time.Time  `json:"last_heartbeat"`
}

// CompleteEventData는 완료 이벤트 데이터를 나타내는 구조체
type CompleteEventData struct {
	RequestID uint64    `json:"request_id"`
	Nonce     uint64    `json:"nonce"`
	Timestamp uint64    `json:"timestamp"`
	BlockTime time.Time `json:"block_time"`
}

// SchedulerMetrics는 스케줄러 메트릭을 나타내는 구조체
type SchedulerMetrics struct {
	// 작업 통계
	TotalJobs     int64 `json:"total_jobs"`
	ActiveJobs    int64 `json:"active_jobs"`
	CompletedJobs int64 `json:"completed_jobs"`
	FailedJobs    int64 `json:"failed_jobs"`
	PendingJobs   int64 `json:"pending_jobs"`

	// 이벤트 통계
	EventsProcessed int64 `json:"events_processed"`
	EventsIgnored   int64 `json:"events_ignored"`
	EventsFailed    int64 `json:"events_failed"`

	// 성능 통계
	AvgJobDuration   time.Duration `json:"avg_job_duration"`
	AvgJobsPerMinute float64       `json:"avg_jobs_per_minute"`
	SuccessRate      float64       `json:"success_rate"`

	// 시간 정보
	StartedAt   time.Time     `json:"started_at"`
	LastEventAt time.Time     `json:"last_event_at"`
	LastJobAt   time.Time     `json:"last_job_at"`
	Uptime      time.Duration `json:"uptime"`
}

// WorkerPoolStats는 워커 풀 통계를 나타내는 구조체
type WorkerPoolStats struct {
	MaxWorkers    int           `json:"max_workers"`
	ActiveWorkers int           `json:"active_workers"`
	QueuedJobs    int           `json:"queued_jobs"`
	CompletedJobs int64         `json:"completed_jobs"`
	FailedJobs    int64         `json:"failed_jobs"`
	AvgDuration   time.Duration `json:"avg_duration"`
}

// SchedulerConfig는 스케줄러 설정을 나타내는 구조체
type SchedulerConfig struct {
	WorkerPoolSize    int           `json:"worker_pool_size"`
	ResultChannelSize int           `json:"result_channel_size"`
	JobTimeout        time.Duration `json:"job_timeout"`
	RetryDelay        time.Duration `json:"retry_delay"`
	MaxRetries        int           `json:"max_retries"`
	MetricsInterval   time.Duration `json:"metrics_interval"`
	HealthCheckInt    time.Duration `json:"health_check_interval"`
}
