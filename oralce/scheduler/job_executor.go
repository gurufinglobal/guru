package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/creachadair/taskgroup"
)

// JobExecutor_impl는 작업 실행기의 구현체
type JobExecutor_impl struct {
	logger     log.Logger
	config     *config.Config
	httpClient *HTTPClient

	// TaskGroup 관리
	taskGroup *taskgroup.Group
	taskFunc  taskgroup.StartFunc

	// 상태 관리
	activeJobs    int32
	totalExecuted int64
	totalFailed   int64

	// 메트릭
	mu             sync.RWMutex
	executionTimes []time.Duration
	maxExecutions  int
}

// NewJobExecutor는 새로운 작업 실행기를 생성
func NewJobExecutor(logger log.Logger, cfg *config.Config) JobExecutor {
	executor := &JobExecutor_impl{
		logger:        logger,
		config:        cfg,
		httpClient:    NewHTTPClient(logger, cfg),
		maxExecutions: cfg.WorkerPoolSize(),
	}

	// TaskGroup 초기화
	executor.taskGroup, executor.taskFunc = taskgroup.New(nil).Limit(cfg.WorkerPoolSize())

	return executor
}

// Execute는 단일 작업을 동기적으로 실행
func (je *JobExecutor_impl) Execute(ctx context.Context, job *OracleJob) (*JobResult, error) {
	if job == nil {
		return nil, &JobExecutionError{
			JobID:   "",
			Phase:   "validation",
			Message: "job cannot be nil",
		}
	}

	// 동시 실행 제한 확인
	if atomic.LoadInt32(&je.activeJobs) >= int32(je.maxExecutions) {
		return nil, &JobExecutionError{
			JobID:   job.ID,
			Phase:   "capacity",
			Message: "executor at maximum capacity",
		}
	}

	startTime := time.Now()
	atomic.AddInt32(&je.activeJobs, 1)
	defer atomic.AddInt32(&je.activeJobs, -1)

	je.logger.Debug("executing job",
		"job_id", job.ID,
		"request_id", job.RequestID,
		"url", job.URL)

	// 실행 컨텍스트 생성 (타임아웃 적용)
	execCtx, cancel := context.WithTimeout(ctx, je.config.WorkerTimeout())
	defer cancel()

	// 실제 작업 실행
	result, err := je.executeJob(execCtx, job, startTime)

	// 실행 시간 기록
	duration := time.Since(startTime)
	je.recordExecutionTime(duration)

	if err != nil {
		atomic.AddInt64(&je.totalFailed, 1)
		je.logger.Error("job execution failed",
			"job_id", job.ID,
			"duration", duration,
			"error", err)

		return &JobResult{
			JobID:      job.ID,
			RequestID:  job.RequestID,
			Success:    false,
			Error:      err,
			ExecutedAt: startTime,
			Duration:   duration,
		}, nil // 에러를 결과에 포함하므로 nil 반환
	}

	atomic.AddInt64(&je.totalExecuted, 1)
	je.logger.Debug("job executed successfully",
		"job_id", job.ID,
		"duration", duration,
		"data_length", len(result.Data))

	return result, nil
}

// ExecuteAsync는 작업을 비동기적으로 실행
func (je *JobExecutor_impl) ExecuteAsync(ctx context.Context, job *OracleJob, resultCh chan<- *JobResult) error {
	if job == nil {
		return &JobExecutionError{
			JobID:   "",
			Phase:   "validation",
			Message: "job cannot be nil",
		}
	}

	if resultCh == nil {
		return &JobExecutionError{
			JobID:   job.ID,
			Phase:   "validation",
			Message: "result channel cannot be nil",
		}
	}

	// TaskGroup에 작업 제출
	je.taskFunc(func() error {
		result, err := je.Execute(ctx, job)
		if err != nil {
			// 실행 자체가 실패한 경우 (용량 부족 등)
			result = &JobResult{
				JobID:      job.ID,
				RequestID:  job.RequestID,
				Success:    false,
				Error:      err,
				ExecutedAt: time.Now(),
			}
		}

		// 결과 전송 (논블로킹)
		select {
		case resultCh <- result:
		case <-ctx.Done():
			je.logger.Debug("context cancelled while sending result", "job_id", job.ID)
		default:
			je.logger.Warn("result channel full, dropping result", "job_id", job.ID)
		}

		return nil // TaskGroup 에러는 항상 nil 반환 (결과에 에러 포함)
	})

	return nil
}

// executeJob은 실제 작업 실행 로직
func (je *JobExecutor_impl) executeJob(ctx context.Context, job *OracleJob, startTime time.Time) (*JobResult, error) {
	// 1. HTTP 요청으로 데이터 가져오기
	rawData, err := je.httpClient.FetchData(ctx, job.URL)
	if err != nil {
		return nil, &JobExecutionError{
			JobID:   job.ID,
			Phase:   "fetch",
			Message: "failed to fetch data",
			Cause:   err,
		}
	}

	// 2. JSON 파싱
	jsonData, err := je.httpClient.ParseJSON(rawData)
	if err != nil {
		return nil, &JobExecutionError{
			JobID:   job.ID,
			Phase:   "parse",
			Message: "failed to parse JSON",
			Cause:   err,
		}
	}

	// 3. 데이터 추출
	extractedData, err := je.httpClient.ExtractData(jsonData, job.ParseRule)
	if err != nil {
		return nil, &JobExecutionError{
			JobID:   job.ID,
			Phase:   "extract",
			Message: "failed to extract data",
			Cause:   err,
		}
	}

	// 4. 결과 생성
	result := &JobResult{
		JobID:      job.ID,
		RequestID:  job.RequestID,
		Data:       extractedData,
		Nonce:      job.Nonce + 1, // 다음 Nonce
		Success:    true,
		ExecutedAt: startTime,
		Duration:   time.Since(startTime),
	}

	return result, nil
}

// GetCapacity는 현재 실행 가능한 작업 수를 반환
func (je *JobExecutor_impl) GetCapacity() int {
	active := atomic.LoadInt32(&je.activeJobs)
	return je.maxExecutions - int(active)
}

// GetActiveJobs는 현재 실행 중인 작업 수를 반환
func (je *JobExecutor_impl) GetActiveJobs() int {
	return int(atomic.LoadInt32(&je.activeJobs))
}

// GetStats는 실행기 통계를 반환
func (je *JobExecutor_impl) GetStats() JobExecutorStats {
	je.mu.RLock()
	defer je.mu.RUnlock()

	stats := JobExecutorStats{
		MaxCapacity:   je.maxExecutions,
		ActiveJobs:    int(atomic.LoadInt32(&je.activeJobs)),
		TotalExecuted: atomic.LoadInt64(&je.totalExecuted),
		TotalFailed:   atomic.LoadInt64(&je.totalFailed),
	}

	// 평균 실행 시간 계산
	if len(je.executionTimes) > 0 {
		var total time.Duration
		for _, duration := range je.executionTimes {
			total += duration
		}
		stats.AvgExecutionTime = total / time.Duration(len(je.executionTimes))
	}

	// 성공률 계산
	totalJobs := stats.TotalExecuted + stats.TotalFailed
	if totalJobs > 0 {
		stats.SuccessRate = float64(stats.TotalExecuted) / float64(totalJobs)
	}

	return stats
}

// recordExecutionTime은 실행 시간을 기록 (메트릭용)
func (je *JobExecutor_impl) recordExecutionTime(duration time.Duration) {
	je.mu.Lock()
	defer je.mu.Unlock()

	// 최근 100개의 실행 시간만 유지
	const maxRecords = 100
	if len(je.executionTimes) >= maxRecords {
		// 슬라이스 앞부분 제거
		copy(je.executionTimes, je.executionTimes[1:])
		je.executionTimes = je.executionTimes[:maxRecords-1]
	}

	je.executionTimes = append(je.executionTimes, duration)
}

// Shutdown은 실행기를 종료
func (je *JobExecutor_impl) Shutdown(ctx context.Context) error {
	je.logger.Info("shutting down job executor",
		"active_jobs", atomic.LoadInt32(&je.activeJobs))

	// TaskGroup 종료 대기
	je.taskGroup.Wait()
	return nil
}

// JobExecutorStats는 작업 실행기 통계를 나타내는 구조체
type JobExecutorStats struct {
	MaxCapacity      int           `json:"max_capacity"`
	ActiveJobs       int           `json:"active_jobs"`
	TotalExecuted    int64         `json:"total_executed"`
	TotalFailed      int64         `json:"total_failed"`
	AvgExecutionTime time.Duration `json:"avg_execution_time"`
	SuccessRate      float64       `json:"success_rate"`
}

// JobExecutionError는 작업 실행 에러를 나타내는 구조체
type JobExecutionError struct {
	JobID   string // 작업 ID
	Phase   string // 실행 단계 (validation, capacity, fetch, parse, extract)
	Message string // 에러 메시지
	Cause   error  // 원인 에러
}

// Error는 error 인터페이스 구현
func (e *JobExecutionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("job execution error [%s] for job '%s': %s (cause: %v)",
			e.Phase, e.JobID, e.Message, e.Cause)
	}
	return fmt.Sprintf("job execution error [%s] for job '%s': %s",
		e.Phase, e.JobID, e.Message)
}

// Unwrap은 원인 에러를 반환
func (e *JobExecutionError) Unwrap() error {
	return e.Cause
}

// IsRetryable은 에러가 재시도 가능한지 확인
func (e *JobExecutionError) IsRetryable() bool {
	switch e.Phase {
	case "validation":
		return false // 검증 에러는 재시도 불가
	case "capacity":
		return true // 용량 부족은 재시도 가능
	case "fetch":
		return true // 네트워크 에러는 재시도 가능
	case "parse":
		return false // 파싱 에러는 보통 재시도 불가
	case "extract":
		return false // 추출 에러는 보통 재시도 불가
	default:
		return false
	}
}

// BatchExecutor는 여러 작업을 일괄 실행하는 헬퍼
type BatchExecutor struct {
	executor JobExecutor
	logger   log.Logger
}

// NewBatchExecutor는 새로운 일괄 실행기를 생성
func NewBatchExecutor(executor JobExecutor, logger log.Logger) *BatchExecutor {
	return &BatchExecutor{
		executor: executor,
		logger:   logger,
	}
}

// ExecuteBatch는 여러 작업을 병렬로 실행
func (be *BatchExecutor) ExecuteBatch(ctx context.Context, jobs []*OracleJob) ([]*JobResult, error) {
	if len(jobs) == 0 {
		return nil, nil
	}

	resultCh := make(chan *JobResult, len(jobs))
	errCh := make(chan error, len(jobs))

	// 모든 작업을 비동기로 시작
	for _, job := range jobs {
		if err := be.executor.ExecuteAsync(ctx, job, resultCh); err != nil {
			errCh <- err
		}
	}

	// 결과 수집
	var results []*JobResult
	var errors []error

	timeout := time.After(30 * time.Second) // 전체 타임아웃
	expectedResults := len(jobs)

resultsLoop:
	for i := 0; i < expectedResults; i++ {
		select {
		case result := <-resultCh:
			results = append(results, result)

		case err := <-errCh:
			errors = append(errors, err)

		case <-timeout:
			be.logger.Warn("batch execution timeout",
				"total_jobs", len(jobs),
				"completed", len(results),
				"errors", len(errors))
			break resultsLoop

		case <-ctx.Done():
			be.logger.Info("batch execution cancelled",
				"total_jobs", len(jobs),
				"completed", len(results))
			return results, ctx.Err()
		}
	}

	be.logger.Info("batch execution completed",
		"total_jobs", len(jobs),
		"successful", len(results),
		"errors", len(errors))

	// 에러가 있으면 첫 번째 에러 반환
	if len(errors) > 0 {
		return results, errors[0]
	}

	return results, nil
}

// HTTPClient는 HTTP 요청을 처리하는 클라이언트
type HTTPClient struct {
	logger log.Logger
	config *config.Config
}

// NewHTTPClient는 새로운 HTTP 클라이언트를 생성
func NewHTTPClient(logger log.Logger, cfg *config.Config) *HTTPClient {
	return &HTTPClient{
		logger: logger,
		config: cfg,
	}
}

// FetchData는 URL에서 데이터를 가져옴
func (hc *HTTPClient) FetchData(ctx context.Context, url string) ([]byte, error) {
	// 실제 구현은 기존 worker/client.go의 fetchRawData와 유사
	// 여기서는 인터페이스만 정의
	return nil, fmt.Errorf("not implemented")
}

// ParseJSON은 JSON 데이터를 파싱
func (hc *HTTPClient) ParseJSON(data []byte) (map[string]any, error) {
	// 실제 구현은 기존 worker/client.go의 parseRawData와 유사
	return nil, fmt.Errorf("not implemented")
}

// ExtractData는 JSON에서 데이터를 추출
func (hc *HTTPClient) ExtractData(data map[string]any, path string) (string, error) {
	// 실제 구현은 기존 worker/client.go의 extractDataByPath와 유사
	return "", fmt.Errorf("not implemented")
}
