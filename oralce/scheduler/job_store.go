package scheduler

import (
	"sync"
	"time"

	"cosmossdk.io/log"
)

// ConcurrentJobStore는 스레드 안전한 작업 저장소 구현
type ConcurrentJobStore struct {
	jobs   map[string]*OracleJob
	mu     sync.RWMutex
	logger log.Logger

	// 메트릭
	totalStored  int64
	totalDeleted int64
	totalUpdated int64
}

// NewJobStore는 새로운 작업 저장소를 생성
func NewJobStore(logger log.Logger) JobStore {
	return &ConcurrentJobStore{
		jobs:   make(map[string]*OracleJob),
		logger: logger,
	}
}

// Store는 작업을 저장
func (js *ConcurrentJobStore) Store(jobID string, job *OracleJob) error {
	if job == nil {
		return &JobStoreError{
			Operation: "store",
			JobID:     jobID,
			Message:   "job cannot be nil",
		}
	}

	if job.ID == "" {
		return &JobStoreError{
			Operation: "store",
			JobID:     job.ID,
			Message:   "job ID cannot be empty",
		}
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	// 작업 복사본 생성 (불변성 보장)
	jobCopy := *job
	jobCopy.UpdatedAt = time.Now()

	// 새 작업인 경우 생성 시간 설정
	if _, exists := js.jobs[job.ID]; !exists {
		jobCopy.CreatedAt = time.Now()
		js.totalStored++
	} else {
		js.totalUpdated++
	}

	js.jobs[job.ID] = &jobCopy

	js.logger.Debug("job stored",
		"job_id", job.ID,
		"request_id", job.RequestID,
		"status", job.ExecutionState.Status)

	return nil
}

// Get은 작업 ID로 작업을 조회
func (js *ConcurrentJobStore) Get(jobID string) (*OracleJob, bool) {
	if jobID == "" {
		return nil, false
	}

	js.mu.RLock()
	defer js.mu.RUnlock()

	job, exists := js.jobs[jobID]
	if !exists {
		return nil, false
	}

	// 복사본 반환 (불변성 보장)
	jobCopy := *job
	return &jobCopy, true
}

// Update는 기존 작업을 업데이트
func (js *ConcurrentJobStore) Update(jobID string, updater func(*OracleJob) error) error {
	if jobID == "" {
		return &JobStoreError{
			Operation: "update",
			JobID:     jobID,
			Message:   "job ID cannot be empty",
		}
	}

	if updater == nil {
		return &JobStoreError{
			Operation: "update",
			JobID:     jobID,
			Message:   "updater function cannot be nil",
		}
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	job, exists := js.jobs[jobID]
	if !exists {
		return &JobStoreError{
			Operation: "update",
			JobID:     jobID,
			Message:   "job not found",
		}
	}

	// 작업 복사본에 업데이트 적용
	jobCopy := *job
	if err := updater(&jobCopy); err != nil {
		return &JobStoreError{
			Operation: "update",
			JobID:     jobID,
			Message:   "updater function failed: " + err.Error(),
			Cause:     err,
		}
	}

	// 업데이트 시간 설정
	jobCopy.UpdatedAt = time.Now()
	js.jobs[jobID] = &jobCopy
	js.totalUpdated++

	js.logger.Debug("job updated",
		"job_id", jobID,
		"status", jobCopy.ExecutionState.Status)

	return nil
}

// Delete는 작업을 삭제
func (js *ConcurrentJobStore) Delete(jobID string) error {
	if jobID == "" {
		return &JobStoreError{
			Operation: "delete",
			JobID:     jobID,
			Message:   "job ID cannot be empty",
		}
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	if _, exists := js.jobs[jobID]; !exists {
		return &JobStoreError{
			Operation: "delete",
			JobID:     jobID,
			Message:   "job not found",
		}
	}

	delete(js.jobs, jobID)
	js.totalDeleted++

	js.logger.Debug("job deleted", "job_id", jobID)
	return nil
}

// List는 모든 작업을 나열
func (js *ConcurrentJobStore) List() []*OracleJob {
	js.mu.RLock()
	defer js.mu.RUnlock()

	jobs := make([]*OracleJob, 0, len(js.jobs))
	for _, job := range js.jobs {
		// 복사본 생성
		jobCopy := *job
		jobs = append(jobs, &jobCopy)
	}

	return jobs
}

// ListByStatus는 특정 상태의 작업들을 나열
func (js *ConcurrentJobStore) ListByStatus(status JobStatus) []*OracleJob {
	js.mu.RLock()
	defer js.mu.RUnlock()

	var jobs []*OracleJob
	for _, job := range js.jobs {
		if job.ExecutionState.Status == status {
			jobCopy := *job
			jobs = append(jobs, &jobCopy)
		}
	}

	return jobs
}

// ListReadyJobs는 실행 준비된 작업들을 나열
func (js *ConcurrentJobStore) ListReadyJobs(now time.Time) []*OracleJob {
	js.mu.RLock()
	defer js.mu.RUnlock()

	var readyJobs []*OracleJob
	for _, job := range js.jobs {
		if js.isJobReady(job, now) {
			jobCopy := *job
			readyJobs = append(readyJobs, &jobCopy)
		}
	}

	// 우선순위별로 정렬 (높은 우선순위 먼저)
	sortJobsByPriority(readyJobs)

	return readyJobs
}

// isJobReady는 작업이 실행 준비되었는지 확인
func (js *ConcurrentJobStore) isJobReady(job *OracleJob, now time.Time) bool {
	// 활성화되지 않은 작업은 실행하지 않음
	if job.Status != 1 { // oracletypes.RequestStatus_REQUEST_STATUS_ENABLED
		return false
	}

	// 이미 실행 중인 작업은 실행하지 않음
	if job.ExecutionState.Status == JobStatusExecuting ||
		job.ExecutionState.Status == JobStatusRetrying {
		return false
	}

	// 실행 시간이 되지 않은 작업은 실행하지 않음
	if job.NextRunTime.After(now) {
		return false
	}

	return true
}

// Count는 총 작업 수를 반환
func (js *ConcurrentJobStore) Count() int {
	js.mu.RLock()
	defer js.mu.RUnlock()

	return len(js.jobs)
}

// GetStats는 저장소 통계를 반환
func (js *ConcurrentJobStore) GetStats() JobStoreStats {
	js.mu.RLock()
	defer js.mu.RUnlock()

	stats := JobStoreStats{
		TotalJobs:    len(js.jobs),
		TotalStored:  js.totalStored,
		TotalDeleted: js.totalDeleted,
		TotalUpdated: js.totalUpdated,
	}

	// 상태별 카운트
	statusCounts := make(map[JobStatus]int)
	for _, job := range js.jobs {
		statusCounts[job.ExecutionState.Status]++
	}

	stats.StatusCounts = statusCounts
	return stats
}

// Cleanup은 오래된 완료/실패 작업들을 정리
func (js *ConcurrentJobStore) Cleanup(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	cutoffTime := time.Now().Add(-maxAge)
	var toDelete []string

	for jobID, job := range js.jobs {
		// 완료되거나 실패한 작업 중 오래된 것들만 삭제
		if (job.ExecutionState.Status == JobStatusCompleted ||
			job.ExecutionState.Status == JobStatusFailed) &&
			job.UpdatedAt.Before(cutoffTime) {
			toDelete = append(toDelete, jobID)
		}
	}

	for _, jobID := range toDelete {
		delete(js.jobs, jobID)
		js.totalDeleted++
	}

	if len(toDelete) > 0 {
		js.logger.Info("cleaned up old jobs",
			"deleted_count", len(toDelete),
			"max_age", maxAge)
	}

	return len(toDelete)
}

// 헬퍼 함수들

// sortJobsByPriority는 작업들을 우선순위별로 정렬
func sortJobsByPriority(jobs []*OracleJob) {
	// 간단한 버블 정렬 (작은 리스트에 적합)
	n := len(jobs)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			// 높은 우선순위가 먼저 오도록 정렬
			if jobs[j].Priority < jobs[j+1].Priority {
				jobs[j], jobs[j+1] = jobs[j+1], jobs[j]
			} else if jobs[j].Priority == jobs[j+1].Priority {
				// 우선순위가 같으면 다음 실행 시간이 빠른 것 먼저
				if jobs[j].NextRunTime.After(jobs[j+1].NextRunTime) {
					jobs[j], jobs[j+1] = jobs[j+1], jobs[j]
				}
			}
		}
	}
}

// 에러 타입 정의

// JobStoreError는 작업 저장소 에러를 나타내는 구조체
type JobStoreError struct {
	Operation string // 실행한 작업 (store, get, update, delete)
	JobID     string // 관련 작업 ID
	Message   string // 에러 메시지
	Cause     error  // 원인 에러
}

// Error는 error 인터페이스 구현
func (e *JobStoreError) Error() string {
	if e.Cause != nil {
		return "job store " + e.Operation + " error for job '" + e.JobID + "': " +
			e.Message + " (cause: " + e.Cause.Error() + ")"
	}
	return "job store " + e.Operation + " error for job '" + e.JobID + "': " + e.Message
}

// Unwrap은 원인 에러를 반환
func (e *JobStoreError) Unwrap() error {
	return e.Cause
}

// GetReadyJobs는 실행 준비된 작업들을 반환
func (js *ConcurrentJobStore) GetReadyJobs(now time.Time) []*OracleJob {
	return js.ListReadyJobs(now)
}

// JobStoreStats는 작업 저장소 통계를 나타내는 구조체
type JobStoreStats struct {
	TotalJobs    int               `json:"total_jobs"`
	TotalStored  int64             `json:"total_stored"`
	TotalDeleted int64             `json:"total_deleted"`
	TotalUpdated int64             `json:"total_updated"`
	StatusCounts map[JobStatus]int `json:"status_counts"`
}

// MemoryJobStore는 메모리 기반 작업 저장소 (테스트용)
type MemoryJobStore struct {
	*ConcurrentJobStore
}

// NewMemoryJobStore는 메모리 기반 작업 저장소를 생성
func NewMemoryJobStore(logger log.Logger) JobStore {
	return &MemoryJobStore{
		ConcurrentJobStore: &ConcurrentJobStore{
			jobs:   make(map[string]*OracleJob),
			logger: logger,
		},
	}
}

// PersistentJobStore는 영구 저장소 기반 작업 저장소 (미래 확장용)
type PersistentJobStore struct {
	*ConcurrentJobStore
	// 미래에 데이터베이스 연결 등을 추가
}

// NewPersistentJobStore는 영구 저장소 기반 작업 저장소를 생성
func NewPersistentJobStore(logger log.Logger) JobStore {
	// 현재는 메모리 저장소와 동일하게 구현
	// 미래에 실제 영구 저장소(DB) 연동 로직 추가
	return &PersistentJobStore{
		ConcurrentJobStore: &ConcurrentJobStore{
			jobs:   make(map[string]*OracleJob),
			logger: logger,
		},
	}
}
