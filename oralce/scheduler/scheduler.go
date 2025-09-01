package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// EventScheduler_impl는 이벤트 기반 스케줄러 구현
type EventScheduler_impl struct {
	// Core dependencies
	eventParser EventParser
	jobExecutor JobExecutor
	jobStore    JobStore
	config      config.Config
	logger      log.Logger

	// Channels
	resultCh chan *JobResult

	// State
	isRunning atomic.Bool
	startedAt time.Time

	// Scheduling
	scheduleTicker *time.Ticker

	// Metrics
	metrics *SchedulerMetrics
}

// NewEventScheduler는 새로운 이벤트 스케줄러를 생성
func NewEventScheduler(
	eventParser EventParser,
	jobExecutor JobExecutor,
	config config.Config,
	resultCh chan *JobResult,
	logger log.Logger,
) *EventScheduler_impl {
	jobStore := NewJobStore(logger)

	return &EventScheduler_impl{
		eventParser: eventParser,
		jobExecutor: jobExecutor,
		jobStore:    jobStore,
		config:      config,
		resultCh:    resultCh,
		logger:      logger,
		metrics: &SchedulerMetrics{
			StartedAt: time.Now(),
		},
	}
}

// Start는 스케줄러를 시작
func (s *EventScheduler_impl) Start(ctx context.Context) error {
	if s.isRunning.Load() {
		return fmt.Errorf("scheduler is already running")
	}

	s.startedAt = time.Now()
	s.isRunning.Store(true)

	// 기존 작업들을 chain에서 로드
	if err := s.loadExistingJobs(ctx); err != nil {
		s.logger.Error("failed to load existing jobs", "error", err)
		return fmt.Errorf("failed to load existing jobs: %w", err)
	}

	// 주기적 스케줄링 시작 (1초마다 체크)
	s.scheduleTicker = time.NewTicker(1 * time.Second)
	go s.runPeriodicScheduler(ctx)

	s.logger.Info("event scheduler started")
	return nil
}

// Stop는 스케줄러를 중지
func (s *EventScheduler_impl) Stop(ctx context.Context) error {
	if !s.isRunning.Load() {
		return fmt.Errorf("scheduler is not running")
	}

	s.isRunning.Store(false)

	// 스케줄링 타이머 중지
	if s.scheduleTicker != nil {
		s.scheduleTicker.Stop()
	}

	s.logger.Info("event scheduler stopped",
		"uptime", time.Since(s.startedAt),
		"total_jobs", s.metrics.TotalJobs,
		"completed_jobs", s.metrics.CompletedJobs)

	return nil
}

// ProcessEvent는 블록체인 이벤트를 TaskGroup으로 즉시 처리
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

	s.logger.Debug("submitting event to taskgroup", "query", event.Query)

	// TaskGroup에 이벤트 처리 작업 제출 (Job 등록/업데이트)
	s.jobExecutor.SubmitTask(func() error {
		return s.processEventToJob(ctx, event)
	})

	return nil
}

// processEventToJob는 이벤트를 Job으로 변환하고 저장
func (s *EventScheduler_impl) processEventToJob(ctx context.Context, event coretypes.ResultEvent) error {
	s.logger.Debug("processing event to job", "query", event.Query)

	// 1. 이벤트 파싱 및 Job 생성
	job, err := s.parseEventToJob(ctx, event)
	if err != nil {
		s.logger.Error("failed to parse event to job", "error", err)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsFailed++
		})
		return nil
	}

	if job == nil {
		// 이 노드가 담당하지 않는 job 또는 Complete 이벤트
		s.logger.Debug("job not assigned to this node or complete event processed")
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.EventsIgnored++
		})
		return nil
	}

	// 2. Job 저장 (RequestID 기반)
	requestKey := fmt.Sprintf("req_%d", job.RequestID)

	// 기존 job이 있는지 확인
	if _, exists := s.jobStore.Get(requestKey); exists {
		// 기존 job 업데이트 (nonce 및 일부 값만)
		s.jobStore.Update(requestKey, func(j *OracleJob) error {
			j.Nonce = job.Nonce
			j.Status = job.Status
			j.UpdatedAt = time.Now()
			// Period와 URL 등은 변경되지 않음
			return nil
		})

		s.logger.Info("updated existing job",
			"request_id", job.RequestID,
			"nonce", job.Nonce)
	} else {
		// 새 job 저장
		job.NextRunTime = time.Now() // 즉시 실행 가능
		if err := s.jobStore.Store(requestKey, job); err != nil {
			s.logger.Error("failed to store job", "request_id", job.RequestID, "error", err)
			s.updateMetrics(func(m *SchedulerMetrics) {
				m.EventsFailed++
			})
			return nil
		}

		s.updateMetrics(func(m *SchedulerMetrics) {
			m.TotalJobs++
		})

		s.logger.Info("stored new job",
			"request_id", job.RequestID,
			"period", job.Period,
			"url", job.URL)
	}

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.EventsProcessed++
	})

	return nil
}

// executeJobToResult는 Job을 실행하여 결과까지 처리
func (s *EventScheduler_impl) executeJobToResult(ctx context.Context, job *OracleJob) error {
	startTime := time.Now()

	s.logger.Info("executing job",
		"request_id", job.RequestID,
		"url", job.URL,
		"parse_rule", job.ParseRule)

	// 1. Job 상태 업데이트 (실행 중)
	requestKey := fmt.Sprintf("req_%d", job.RequestID)
	s.jobStore.Update(requestKey, func(j *OracleJob) error {
		j.ExecutionState.Status = JobStatusExecuting
		j.ExecutionState.StartTime = &startTime
		j.ExecutionState.LastHeartbeat = &startTime
		return nil
	})

	// 2. 외부 API에서 데이터 가져오기
	rawData, err := s.jobExecutor.FetchData(ctx, job.URL)
	if err != nil {
		s.logger.Error("failed to fetch external data",
			"request_id", job.RequestID,
			"url", job.URL,
			"error", err)

		// 실패 상태 업데이트
		s.jobStore.Update(requestKey, func(j *OracleJob) error {
			j.ExecutionState.Status = JobStatusFailed
			endTime := time.Now()
			j.ExecutionState.EndTime = &endTime
			j.FailureCount++
			j.LastError = err.Error()
			return nil
		})

		// 실패 결과 전송
		s.sendJobResult(&JobResult{
			JobID:      job.ID,
			RequestID:  job.RequestID,
			Data:       "",
			Nonce:      job.Nonce,
			Success:    false,
			Error:      err,
			ExecutedAt: startTime,
			Duration:   time.Since(startTime),
		})

		s.updateMetrics(func(m *SchedulerMetrics) {
			m.FailedJobs++
		})
		return nil
	}

	// 3. 데이터 파싱
	extractedData, err := s.jobExecutor.ParseAndExtract(rawData, job.ParseRule)
	if err != nil {
		s.logger.Error("failed to parse and extract data",
			"request_id", job.RequestID,
			"parse_rule", job.ParseRule,
			"error", err)

		// 실패 상태 업데이트
		s.jobStore.Update(requestKey, func(j *OracleJob) error {
			j.ExecutionState.Status = JobStatusFailed
			endTime := time.Now()
			j.ExecutionState.EndTime = &endTime
			j.FailureCount++
			j.LastError = err.Error()
			return nil
		})

		// 실패 결과 전송
		s.sendJobResult(&JobResult{
			JobID:      job.ID,
			RequestID:  job.RequestID,
			Data:       "",
			Nonce:      job.Nonce,
			Success:    false,
			Error:      err,
			ExecutedAt: startTime,
			Duration:   time.Since(startTime),
		})

		s.updateMetrics(func(m *SchedulerMetrics) {
			m.FailedJobs++
		})
		return nil
	}

	// 4. 성공 상태 업데이트
	s.jobStore.Update(requestKey, func(j *OracleJob) error {
		j.ExecutionState.Status = JobStatusCompleted
		endTime := time.Now()
		j.ExecutionState.EndTime = &endTime
		j.RunCount++
		return nil
	})

	// 5. 성공 결과 전송
	s.sendJobResult(&JobResult{
		JobID:      job.ID,
		RequestID:  job.RequestID,
		Data:       extractedData,
		Nonce:      job.Nonce,
		Success:    true,
		Error:      nil,
		ExecutedAt: startTime,
		Duration:   time.Since(startTime),
	})

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.CompletedJobs++
	})

	s.logger.Info("job completed successfully",
		"request_id", job.RequestID,
		"duration", time.Since(startTime),
		"data_length", len(extractedData))

	return nil
}

// parseEventToJob은 이벤트를 OracleJob으로 변환
func (s *EventScheduler_impl) parseEventToJob(ctx context.Context, event coretypes.ResultEvent) (*OracleJob, error) {
	switch event.Query {
	case "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'":
		return s.handleRegisterEvent(ctx, event)

	case "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'":
		return s.handleUpdateEvent(ctx, event)

	case "tm.event='NewBlock' AND guru.oracle.v1.CompleteOracleDataSet.request_id EXISTS":
		// Complete 이벤트는 별도 처리 (다음 실행 시간 계산)
		return nil, s.handleCompleteEvent(ctx, event)

	default:
		s.logger.Debug("unsupported event type", "query", event.Query)
		return nil, nil
	}
}

// handleRegisterEvent는 등록 이벤트를 처리하여 Job 생성
func (s *EventScheduler_impl) handleRegisterEvent(ctx context.Context, event coretypes.ResultEvent) (*OracleJob, error) {
	doc, err := s.eventParser.ParseRegisterEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("failed to parse register event: %w", err)
	}

	job, err := s.eventParser.ConvertToJob(doc, s.config.Address().String(), time.Now())
	if err != nil {
		// 이 노드가 담당하지 않는 job인 경우
		s.logger.Debug("job not assigned to this node", "error", err)
		return nil, nil
	}

	return job, nil
}

// handleUpdateEvent는 업데이트 이벤트를 처리하여 Job 생성
func (s *EventScheduler_impl) handleUpdateEvent(ctx context.Context, event coretypes.ResultEvent) (*OracleJob, error) {
	doc, err := s.eventParser.ParseUpdateEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("failed to parse update event: %w", err)
	}

	job, err := s.eventParser.ConvertToJob(doc, s.config.Address().String(), time.Now())
	if err != nil {
		// 이 노드가 담당하지 않는 job인 경우
		s.logger.Debug("job not assigned to this node", "error", err)
		return nil, nil
	}

	return job, nil
}

// handleCompleteEvent는 완료 이벤트를 처리하여 다음 실행 시간 계산
func (s *EventScheduler_impl) handleCompleteEvent(ctx context.Context, event coretypes.ResultEvent) error {
	completeData, err := s.eventParser.ParseCompleteEvent(event)
	if err != nil {
		s.logger.Error("failed to parse complete event", "error", err)
		return err
	}

	// RequestID 기반으로 Job 찾기
	requestKey := fmt.Sprintf("req_%s", completeData.RequestID)
	job, exists := s.jobStore.Get(requestKey)
	if !exists {
		s.logger.Debug("job not found for complete event", "request_id", completeData.RequestID)
		return nil
	}

	// 다음 실행 시간 계산 (현재 시간 + period)
	nextRunTime := time.Now().Add(time.Duration(job.Period) * time.Second)

	// Job의 다음 실행 시간 업데이트
	s.jobStore.Update(requestKey, func(j *OracleJob) error {
		j.NextRunTime = nextRunTime
		j.ExecutionState.Status = JobStatusPending
		return nil
	})

	s.logger.Info("scheduled next execution after complete event",
		"request_id", completeData.RequestID,
		"next_run_time", nextRunTime,
		"period", job.Period)

	return nil
}

// sendJobResult는 작업 결과를 결과 채널로 전송
func (s *EventScheduler_impl) sendJobResult(result *JobResult) {
	select {
	case s.resultCh <- result:
		s.logger.Debug("job result sent to channel",
			"job_id", result.JobID,
			"success", result.Success)
	default:
		s.logger.Warn("result channel is full, dropping result",
			"job_id", result.JobID)
	}
}

// GetResultChannel는 결과 채널을 반환
func (s *EventScheduler_impl) GetResultChannel() <-chan *JobResult {
	return s.resultCh
}

// GetMetrics는 현재 메트릭스를 반환
func (s *EventScheduler_impl) GetMetrics() *SchedulerMetrics {
	return s.metrics
}

// updateMetrics는 스레드 안전하게 메트릭스를 업데이트
func (s *EventScheduler_impl) updateMetrics(updateFunc func(*SchedulerMetrics)) {
	updateFunc(s.metrics)
}

// GetJobStatus는 특정 작업의 상태를 반환 (인터페이스 구현)
func (s *EventScheduler_impl) GetJobStatus(jobID string) (JobStatus, bool) {
	// 새로운 설계에서는 JobStore를 사용하지 않으므로 항상 false 반환
	return JobStatus(0), false
}

// IsRunning은 스케줄러 실행 상태를 반환 (인터페이스 구현)
func (s *EventScheduler_impl) IsRunning() bool {
	return s.isRunning.Load()
}

// loadExistingJobs는 시작시 chain에서 기존 작업들을 로드
func (s *EventScheduler_impl) loadExistingJobs(ctx context.Context) error {
	s.logger.Info("loading existing jobs from chain")

	// TODO: QueryClient를 통해 chain에서 현재 활성 Oracle 요청들을 조회
	// 임시로 빈 구현 - 실제로는 QueryClient.OracleData() 등을 사용

	s.logger.Info("loaded existing jobs", "count", 0)
	return nil
}

// runPeriodicScheduler는 주기적으로 실행할 작업들을 체크
func (s *EventScheduler_impl) runPeriodicScheduler(ctx context.Context) {
	s.logger.Info("started periodic scheduler")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("periodic scheduler stopped by context")
			return

		case <-s.scheduleTicker.C:
			if !s.isRunning.Load() {
				s.logger.Info("periodic scheduler stopped")
				return
			}

			s.checkAndExecuteReadyJobs(ctx)
		}
	}
}

// checkAndExecuteReadyJobs는 실행 준비된 작업들을 찾아서 실행
func (s *EventScheduler_impl) checkAndExecuteReadyJobs(ctx context.Context) {
	now := time.Now()
	readyJobs := s.jobStore.GetReadyJobs(now)

	if len(readyJobs) == 0 {
		return
	}

	s.logger.Debug("found ready jobs", "count", len(readyJobs))

	for _, job := range readyJobs {
		// Job이 이미 실행 중인지 확인
		if job.ExecutionState.Status == JobStatusExecuting {
			s.logger.Debug("job already executing", "request_id", job.RequestID)
			continue
		}

		// TaskGroup에 실행 작업 제출
		jobCopy := *job // job 복사
		s.jobExecutor.SubmitTask(func() error {
			return s.executeJobToResult(ctx, &jobCopy)
		})

		s.logger.Debug("submitted job for execution", "request_id", job.RequestID)
	}
}
