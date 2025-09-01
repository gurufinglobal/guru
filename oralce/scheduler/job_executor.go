package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
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

// SubmitTask는 임의의 태스크를 비동기로 실행
func (je *JobExecutor_impl) SubmitTask(task func() error) {
	je.taskFunc(task)
}

// FetchData는 외부 URL에서 데이터를 가져옴 (HTTPClient 래퍼)
func (je *JobExecutor_impl) FetchData(ctx context.Context, url string) ([]byte, error) {
	return je.httpClient.FetchData(ctx, url)
}

// ParseAndExtract는 JSON 데이터를 파싱하고 경로에 따라 값을 추출
func (je *JobExecutor_impl) ParseAndExtract(rawData []byte, parseRule string) (string, error) {
	// 1. JSON 파싱
	jsonData, err := je.httpClient.ParseJSON(rawData)
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON: %w", err)
	}

	// 2. 경로에 따라 데이터 추출
	return je.httpClient.ExtractData(jsonData, parseRule)
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
	logger     log.Logger
	config     *config.Config
	httpClient *http.Client
}

// NewHTTPClient는 새로운 HTTP 클라이언트를 생성
func NewHTTPClient(logger log.Logger, cfg *config.Config) *HTTPClient {
	return &HTTPClient{
		logger: logger,
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // 30초 타임아웃
		},
	}
}

// FetchData는 URL에서 데이터를 가져옴 (retry 전략 포함)
func (hc *HTTPClient) FetchData(ctx context.Context, url string) ([]byte, error) {
	const (
		maxRetries = 3
		baseDelay  = 1 * time.Second
		maxDelay   = 10 * time.Second
	)

	hc.logger.Debug("fetching data from URL", "url", url, "max_retries", maxRetries)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 지수 백오프로 재시도 지연
			delay := time.Duration(1<<uint(attempt-1)) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			hc.logger.Debug("retrying request after delay",
				"url", url,
				"attempt", attempt,
				"delay", delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		body, err := hc.performSingleRequest(ctx, url)
		if err == nil {
			hc.logger.Debug("successfully fetched data",
				"url", url,
				"attempts", attempt+1,
				"response_size", len(body))
			return body, nil
		}

		lastErr = err
		hc.logger.Warn("HTTP request failed",
			"url", url,
			"attempt", attempt+1,
			"error", err)

		// 컨텍스트가 취소되었으면 재시도하지 않음
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("HTTP request failed after %d attempts: %w", maxRetries+1, lastErr)
}

// performSingleRequest는 단일 HTTP 요청을 수행
func (hc *HTTPClient) performSingleRequest(ctx context.Context, url string) ([]byte, error) {
	// HTTP GET 요청 생성
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// User-Agent 헤더 설정
	req.Header.Set("User-Agent", "Oracle-Daemon/1.0")
	req.Header.Set("Accept", "application/json")

	// 요청 실행
	resp, err := hc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// 응답 상태 코드 확인
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	// 응답 본문 읽기
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// ParseJSON은 JSON 데이터를 파싱
func (hc *HTTPClient) ParseJSON(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data provided")
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		// JSON 파싱 실패 시 디버그 정보 로깅
		hc.logger.Error("failed to parse JSON",
			"error", err,
			"data_preview", string(data[:min(len(data), 200)]))
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	hc.logger.Debug("successfully parsed JSON", "keys", getTopLevelKeys(result))
	return result, nil
}

// getTopLevelKeys는 JSON 객체의 최상위 키들을 반환 (디버깅용)
func getTopLevelKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

// ExtractData는 JSON에서 데이터를 추출
// path는 점(.)으로 구분된 경로 (예: "rates.KRW", "data.price.usd")
func (hc *HTTPClient) ExtractData(data map[string]any, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path provided")
	}

	hc.logger.Debug("extracting data from JSON", "path", path)

	// 경로를 점(.)으로 분할
	pathParts := strings.Split(path, ".")

	// 현재 데이터 포인터
	current := data

	// 경로를 따라 탐색
	for i, part := range pathParts {
		if part == "" {
			return "", fmt.Errorf("empty path segment at position %d", i)
		}

		// 현재 레벨에서 키 찾기
		value, exists := current[part]
		if !exists {
			availableKeys := getTopLevelKeys(current)
			return "", fmt.Errorf("key '%s' not found at path segment %d, available keys: %v",
				part, i, availableKeys)
		}

		// 마지막 경로 세그먼트인 경우
		if i == len(pathParts)-1 {
			// 값을 문자열로 변환
			return convertToString(value)
		}

		// 중간 경로인 경우, map으로 변환 시도
		switch v := value.(type) {
		case map[string]any:
			current = v
		default:
			return "", fmt.Errorf("path segment '%s' at position %d is not an object (type: %T)",
				part, i, value)
		}
	}

	return "", fmt.Errorf("unexpected end of path traversal")
}

// convertToString은 다양한 타입의 값을 문자열로 변환
func convertToString(value any) (string, error) {
	if value == nil {
		return "", fmt.Errorf("value is null")
	}

	switch v := value.(type) {
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	case json.Number:
		return string(v), nil
	default:
		// 복잡한 객체의 경우 JSON으로 마샬링
		if reflect.TypeOf(value).Kind() == reflect.Map ||
			reflect.TypeOf(value).Kind() == reflect.Slice {
			jsonBytes, err := json.Marshal(value)
			if err != nil {
				return "", fmt.Errorf("failed to marshal value to JSON: %w", err)
			}
			return string(jsonBytes), nil
		}

		// 기본적으로 fmt.Sprintf 사용
		return fmt.Sprintf("%v", value), nil
	}
}
