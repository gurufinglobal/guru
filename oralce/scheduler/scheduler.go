package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// EventScheduler_impl는 이벤트 스케줄러의 메인 구현체
type EventScheduler_impl struct {
	logger log.Logger
	config *config.Config

	// 컴포넌트들
	jobStore    JobStore
	jobExecutor JobExecutor
	eventParser EventParser

	// 결과 채널
	resultCh chan *JobResult

	// 상태 관리
	isRunning atomic.Bool
	startedAt time.Time

	// 메트릭
	metrics   SchedulerMetrics
	metricsMu sync.RWMutex

	// 스케줄링
	schedulerTicker *time.Ticker
	metricsTicker   *time.Ticker

	// 종료 채널
	shutdownCh chan struct{}
}

// NewEventScheduler는 새로운 이벤트 스케줄러를 생성
func NewEventScheduler(
	logger log.Logger,
	cfg *config.Config,
	queryClient QueryClient,
) EventScheduler {

	schedulerConfig := SchedulerConfig{
		WorkerPoolSize:    cfg.WorkerPoolSize(),
		ResultChannelSize: cfg.WorkerChannelSize(),
		JobTimeout:        cfg.WorkerTimeout(),
		RetryDelay:        cfg.RetryMaxDelay(),
		MaxRetries:        cfg.RetryMaxAttempts(),
		MetricsInterval:   10 * time.Second,
		HealthCheckInt:    30 * time.Second,
	}

	scheduler := &EventScheduler_impl{
		logger:      logger,
		config:      cfg,
		jobStore:    NewConcurrentJobStore(logger),
		jobExecutor: NewJobExecutor(logger, cfg),
		eventParser: NewEventParser(logger, queryClient),
		resultCh:    make(chan *JobResult, schedulerConfig.ResultChannelSize),
		shutdownCh:  make(chan struct{}),
		metrics: SchedulerMetrics{
			StartedAt: time.Now(),
		},
	}

	return scheduler
}

// Start는 스케줄러를 시작하고 기존 작업들을 로드
func (s *EventScheduler_impl) Start(ctx context.Context) error {
	if s.isRunning.Load() {
		return fmt.Errorf("scheduler is already running")
	}

	s.startedAt = time.Now()
	s.metricsMu.Lock()
	s.metrics.StartedAt = s.startedAt
	s.metricsMu.Unlock()

	// 기존 활성 작업들 로드
	if err := s.loadActiveJobs(ctx); err != nil {
		return fmt.Errorf("failed to load active jobs: %w", err)
	}

	s.isRunning.Store(true)

	// 백그라운드 작업들 시작
	s.schedulerTicker = time.NewTicker(1 * time.Second) // 매초 스케줄링 체크
	s.metricsTicker = time.NewTicker(10 * time.Second)  // 10초마다 메트릭 업데이트

	go s.runJobScheduler(ctx)
	go s.runMetricsUpdater(ctx)
	go s.handleShutdown(ctx)

	s.logger.Info("event scheduler started",
		"worker_pool_size", s.config.WorkerPoolSize(),
		"result_channel_size", cap(s.resultCh))

	return nil
}

// Stop은 모든 작업을 중지하고 리소스를 정리
func (s *EventScheduler_impl) Stop() error {
	if !s.isRunning.CompareAndSwap(true, false) {
		return fmt.Errorf("scheduler is not running")
	}

	s.logger.Info("stopping event scheduler...")

	// 타이머 정지
	if s.schedulerTicker != nil {
		s.schedulerTicker.Stop()
	}
	if s.metricsTicker != nil {
		s.metricsTicker.Stop()
	}

	// 종료 신호
	close(s.shutdownCh)

	// 실행기 종료
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.jobExecutor.Shutdown(ctx); err != nil {
		s.logger.Error("failed to shutdown job executor", "error", err)
	}

	// 결과 채널 정리
	close(s.resultCh)

	s.logger.Info("event scheduler stopped",
		"uptime", time.Since(s.startedAt),
		"total_jobs", s.metrics.TotalJobs,
		"completed_jobs", s.metrics.CompletedJobs)

	return nil
}

// ProcessEvent는 블록체인 이벤트를 처리하여 작업으로 변환
func (s *EventScheduler_impl) ProcessEvent(ctx context.Context, event coretypes.ResultEvent) error {
	if !s.isRunning.Load() {
		return fmt.Errorf("scheduler is not running")
	}

	// 이벤트 타입 확인
	if !s.eventParser.IsEventSupported(event) {
		s.logger.Debug("unsupported event type", "query", event.Query)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsIgnored++
		})
		return nil
	}

	s.logger.Debug("processing event", "query", event.Query)

	switch event.Query {
	case "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'":
		return s.handleRegisterEvent(ctx, event)

	case "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'":
		return s.handleUpdateEvent(ctx, event)

	case "tm.event='NewBlock' AND guru.oracle.v1.CompleteOracleDataSet.request_id EXISTS":
		return s.handleCompleteEvent(ctx, event)

	default:
		s.logger.Debug("unknown event query", "query", event.Query)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsIgnored++
		})
		return nil
	}
}

// handleRegisterEvent는 등록 이벤트를 처리
func (s *EventScheduler_impl) handleRegisterEvent(ctx context.Context, event coretypes.ResultEvent) error {
	doc, err := s.eventParser.ParseRegisterEvent(ctx, event)
	if err != nil {
		s.logger.Error("failed to parse register event", "error", err)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsFailed++
		})
		return err
	}

	// 작업으로 변환
	job, err := s.eventParser.ConvertToJob(doc, s.config.Address().String(), time.Now())
	if err != nil {
		s.logger.Debug("job not assigned to this node or conversion failed", "error", err)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsIgnored++
		})
		return nil // 이 노드 담당이 아니면 에러가 아님
	}

	// 작업 저장
	if err := s.jobStore.Store(job); err != nil {
		s.logger.Error("failed to store job", "job_id", job.ID, "error", err)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsFailed++
		})
		return err
	}

	s.logger.Info("job scheduled from register event",
		"job_id", job.ID,
		"request_id", job.RequestID,
		"next_run", job.NextRunTime)

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.EventsProcessed++
		m.TotalJobs++
		m.PendingJobs++
	})

	return nil
}

// handleUpdateEvent는 업데이트 이벤트를 처리
func (s *EventScheduler_impl) handleUpdateEvent(ctx context.Context, event coretypes.ResultEvent) error {
	doc, err := s.eventParser.ParseUpdateEvent(ctx, event)
	if err != nil {
		s.logger.Error("failed to parse update event", "error", err)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsFailed++
		})
		return err
	}

	// 기존 작업 업데이트 또는 새 작업 생성
	jobID := fmt.Sprintf("job_%d_%d", doc.RequestId, doc.Nonce)

	_, exists := s.jobStore.Get(jobID)
	if exists {
		// 기존 작업 업데이트
		err := s.jobStore.Update(jobID, func(job *OracleJob) error {
			job.Status = doc.Status
			job.Nonce = doc.Nonce
			job.UpdatedAt = time.Now()
			return nil
		})

		if err != nil {
			s.logger.Error("failed to update job", "job_id", jobID, "error", err)
			s.updateMetrics(func(m *SchedulerMetrics) {
				m.EventsFailed++
			})
			return err
		}

		s.logger.Info("job updated from update event", "job_id", jobID)
	} else {
		// 새 작업 생성
		job, err := s.eventParser.ConvertToJob(doc, s.config.Address().String(), time.Now())
		if err != nil {
			s.logger.Debug("job not assigned to this node", "error", err)
			s.updateMetrics(func(m *SchedulerMetrics) {
				m.EventsIgnored++
			})
			return nil
		}

		if err := s.jobStore.Store(job); err != nil {
			s.logger.Error("failed to store new job", "job_id", job.ID, "error", err)
			s.updateMetrics(func(m *SchedulerMetrics) {
				m.EventsFailed++
			})
			return err
		}

		s.logger.Info("new job created from update event", "job_id", job.ID)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.TotalJobs++
			m.PendingJobs++
		})
	}

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.EventsProcessed++
	})

	return nil
}

// handleCompleteEvent는 완료 이벤트를 처리
func (s *EventScheduler_impl) handleCompleteEvent(ctx context.Context, event coretypes.ResultEvent) error {
	completeData, err := s.eventParser.ParseCompleteEvent(event)
	if err != nil {
		s.logger.Error("failed to parse complete event", "error", err)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsFailed++
		})
		return err
	}

	// 해당 작업 찾기 및 업데이트
	jobID := fmt.Sprintf("job_%s_%d", completeData.RequestID, completeData.Nonce)

	err = s.jobStore.Update(jobID, func(job *OracleJob) error {
		// Nonce 업데이트
		job.Nonce = max(job.Nonce, completeData.Nonce)

		// 다음 실행 시간 계산
		job.NextRunTime = completeData.BlockTime.Add(job.Period)
		job.UpdatedAt = time.Now()

		// 상태 초기화
		job.ExecutionState.Status = JobStatusPending
		job.RetryAttempts = 0

		return nil
	})

	if err != nil {
		s.logger.Debug("job not found for complete event",
			"job_id", jobID, "request_id", completeData.RequestID)
		// 완료 이벤트에 해당하는 작업이 없을 수 있음 (다른 노드가 처리한 경우)
	} else {
		s.logger.Info("job updated from complete event",
			"job_id", jobID,
			"new_nonce", completeData.Nonce,
			"next_run", completeData.BlockTime.Add(time.Hour)) // 임시 period
	}

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.EventsProcessed++
	})

	return nil
}

// runJobScheduler는 주기적으로 실행 준비된 작업들을 찾아서 실행
func (s *EventScheduler_impl) runJobScheduler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("job scheduler stopped")
			return
		case <-s.shutdownCh:
			s.logger.Debug("job scheduler shutdown")
			return
		case <-s.schedulerTicker.C:
			s.processReadyJobs(ctx)
		}
	}
}

// processReadyJobs는 실행 준비된 작업들을 처리
func (s *EventScheduler_impl) processReadyJobs(ctx context.Context) {
	now := time.Now()
	readyJobs := s.jobStore.ListReadyJobs(now)

	if len(readyJobs) == 0 {
		return
	}

	s.logger.Debug("found ready jobs", "count", len(readyJobs))

	// 실행 용량 확인
	availableCapacity := s.jobExecutor.GetCapacity()
	if availableCapacity <= 0 {
		s.logger.Debug("no available execution capacity")
		return
	}

	// 용량만큼 작업 실행
	jobsToExecute := readyJobs
	if len(jobsToExecute) > availableCapacity {
		jobsToExecute = readyJobs[:availableCapacity]
	}

	for _, job := range jobsToExecute {
		// 작업 상태를 실행 중으로 변경
		s.jobStore.Update(job.ID, func(j *OracleJob) error {
			j.ExecutionState.Status = JobStatusExecuting
			now := time.Now()
			j.ExecutionState.StartTime = &now
			j.ExecutionState.LastHeartbeat = &now
			return nil
		})

		// 비동기 실행
		if err := s.jobExecutor.ExecuteAsync(ctx, job, s.resultCh); err != nil {
			s.logger.Error("failed to execute job", "job_id", job.ID, "error", err)

			// 실행 실패 시 상태 복원
			s.jobStore.Update(job.ID, func(j *OracleJob) error {
				j.ExecutionState.Status = JobStatusFailed
				j.ExecutionState.EndTime = &now
				j.FailureCount++
				j.LastError = err.Error()
				return nil
			})
		}
	}

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.ActiveJobs = int64(s.jobExecutor.GetActiveJobs())
	})
}

// Results는 완료된 작업 결과를 전달하는 채널 반환
func (s *EventScheduler_impl) Results() <-chan *JobResult {
	return s.resultCh
}

// GetMetrics는 현재 스케줄러 메트릭을 반환
func (s *EventScheduler_impl) GetMetrics() SchedulerMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	metrics := s.metrics
	metrics.Uptime = time.Since(s.startedAt)

	return metrics
}

// GetJobStatus는 특정 작업의 상태를 반환
func (s *EventScheduler_impl) GetJobStatus(jobID string) (JobStatus, bool) {
	job, exists := s.jobStore.Get(jobID)
	if !exists {
		return JobStatusPending, false
	}

	return job.ExecutionState.Status, true
}

// GetAllJobs는 모든 활성 작업들의 상태를 반환
func (s *EventScheduler_impl) GetAllJobs() map[string]JobStatus {
	jobs := s.jobStore.List()
	result := make(map[string]JobStatus)

	for _, job := range jobs {
		result[job.ID] = job.ExecutionState.Status
	}

	return result
}

// IsRunning은 스케줄러 실행 상태를 반환
func (s *EventScheduler_impl) IsRunning() bool {
	return s.isRunning.Load()
}

// loadActiveJobs는 기존 활성 작업들을 로드 (미래 확장용)
func (s *EventScheduler_impl) loadActiveJobs(ctx context.Context) error {
	// 현재는 메모리 기반이므로 기존 작업 없음
	// 미래에 영구 저장소 사용 시 여기서 로드
	s.logger.Info("loading active jobs", "count", 0)
	return nil
}

// runMetricsUpdater는 주기적으로 메트릭을 업데이트
func (s *EventScheduler_impl) runMetricsUpdater(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("metrics updater stopped")
			return
		case <-s.shutdownCh:
			s.logger.Debug("metrics updater shutdown")
			return
		case <-s.metricsTicker.C:
			s.updateJobMetrics()
		}
	}
}

// updateJobMetrics는 작업 기반 메트릭을 업데이트
func (s *EventScheduler_impl) updateJobMetrics() {
	jobs := s.jobStore.List()

	var pending, active, completed, failed int64

	for _, job := range jobs {
		switch job.ExecutionState.Status {
		case JobStatusPending, JobStatusScheduled:
			pending++
		case JobStatusExecuting, JobStatusRetrying:
			active++
		case JobStatusCompleted:
			completed++
		case JobStatusFailed, JobStatusCancelled:
			failed++
		}
	}

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.PendingJobs = pending
		m.ActiveJobs = active
		m.CompletedJobs = completed
		m.FailedJobs = failed

		// 성공률 계산
		total := completed + failed
		if total > 0 {
			m.SuccessRate = float64(completed) / float64(total)
		}
	})
}

// updateMetrics는 메트릭을 안전하게 업데이트
func (s *EventScheduler_impl) updateMetrics(updater func(*SchedulerMetrics)) {
	s.metricsMu.Lock()
	updater(&s.metrics)
	s.metricsMu.Unlock()
}

// handleShutdown은 컨텍스트 취소 시 정리
func (s *EventScheduler_impl) handleShutdown(ctx context.Context) {
	<-ctx.Done()
	s.logger.Info("shutdown signal received")

	if s.isRunning.Load() {
		s.Stop()
	}
}
