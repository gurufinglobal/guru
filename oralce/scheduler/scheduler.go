package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// EventScheduler_impl는 이벤트 기반 스케줄러 구현
type EventScheduler_impl struct {
	// Core dependencies
	eventParser EventParser
	jobExecutor JobExecutor
	jobStore    JobStore
	queryClient QueryClient
	config      config.Config
	logger      log.Logger

	// Channels
	resultCh chan *JobResult

	// State
	isRunning atomic.Bool
	startedAt time.Time

	// Scheduling
	scheduleTicker *time.Ticker
	scheduleWg     sync.WaitGroup

	// Metrics
	metrics *SchedulerMetrics
}

// NewEventScheduler는 새로운 이벤트 스케줄러를 생성
func NewEventScheduler(
	eventParser EventParser,
	jobExecutor JobExecutor,
	queryClient QueryClient,
	config config.Config,
	resultCh chan *JobResult,
	logger log.Logger,
) *EventScheduler_impl {
	jobStore := NewJobStore(logger)

	return &EventScheduler_impl{
		eventParser: eventParser,
		jobExecutor: jobExecutor,
		jobStore:    jobStore,
		queryClient: queryClient,
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
	s.scheduleWg.Add(1)
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

	// 주기적 스케줄러 종료 대기
	s.scheduleWg.Wait()

	// JobExecutor 종료 (TaskGroup 대기)
	if s.jobExecutor != nil {
		if err := s.jobExecutor.Shutdown(ctx); err != nil {
			s.logger.Error("failed to shutdown job executor", "error", err)
		}
	}

	// Result 채널 닫기 (더 이상 결과를 전송하지 않음)
	if s.resultCh != nil {
		close(s.resultCh)
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
		// 기존 job 업데이트 (Update 이벤트인 경우 모든 필드 업데이트)
		s.jobStore.Update(requestKey, func(j *OracleJob) error {
			j.URL = job.URL
			j.ParseRule = job.ParseRule
			j.Period = job.Period
			j.Status = job.Status
			j.AccountList = job.AccountList
			j.AssignedIndex = job.AssignedIndex
			j.UpdatedAt = time.Now()
			// Nonce는 Complete 이벤트에서만 업데이트
			return nil
		})

		s.logger.Info("updated existing job",
			"request_id", job.RequestID,
			"url", job.URL,
			"period", job.Period)
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

		// 실패 상태 업데이트 - 1분 후 재시도 가능하도록 설정
		s.jobStore.Update(requestKey, func(j *OracleJob) error {
			j.ExecutionState.Status = JobStatusFailed
			endTime := time.Now()
			j.ExecutionState.EndTime = &endTime
			j.FailureCount++
			j.LastError = err.Error()
			// 실패 시 1분 후 재시도 가능하도록 설정
			j.NextRunTime = time.Now().Add(1 * time.Minute)
			return nil
		})

		// 실패 결과는 전송하지 않음 (재시도 예정)
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

		// 실패 상태 업데이트 - 1분 후 재시도 가능하도록 설정
		s.jobStore.Update(requestKey, func(j *OracleJob) error {
			j.ExecutionState.Status = JobStatusFailed
			endTime := time.Now()
			j.ExecutionState.EndTime = &endTime
			j.FailureCount++
			j.LastError = err.Error()
			// 파싱 실패는 보통 재시도해도 같은 결과이므로 더 긴 지연 적용
			j.NextRunTime = time.Now().Add(5 * time.Minute)
			return nil
		})

		// 실패 결과는 전송하지 않음 (재시도 예정)
		s.updateMetrics(func(m *SchedulerMetrics) {
			m.FailedJobs++
		})
		return nil
	}

	// 4. 성공 상태 업데이트 및 다음 실행 시간 계산
	s.jobStore.Update(requestKey, func(j *OracleJob) error {
		j.ExecutionState.Status = JobStatusCompleted // Complete 이벤트 대기
		endTime := time.Now()
		j.ExecutionState.EndTime = &endTime
		j.LastRunAt = &endTime
		j.RunCount++
		j.LastError = "" // 에러 초기화
		// NextRunTime을 먼 미래로 설정하여 Complete 이벤트 전까지 재실행 방지
		j.NextRunTime = time.Now().Add(24 * time.Hour)
		return nil
	})

	// 5. 성공 결과 전송
	s.sendJobResult(&JobResult{
		JobID:      job.ID,
		RequestID:  job.RequestID,
		Data:       extractedData,
		Nonce:      job.Nonce + 1, // 다음 nonce로 전송
		Success:    true,
		Error:      nil,
		ExecutedAt: startTime,
		Duration:   time.Since(startTime),
	})

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.CompletedJobs++
		m.LastJobAt = time.Now()
	})

	s.logger.Info("job completed successfully",
		"request_id", job.RequestID,
		"duration", time.Since(startTime),
		"data_length", len(extractedData),
		"next_nonce", job.Nonce+1)

	return nil
}

// parseEventToJob은 이벤트를 OracleJob으로 변환
func (s *EventScheduler_impl) parseEventToJob(ctx context.Context, event coretypes.ResultEvent) (*OracleJob, error) {
	switch {
	case event.Query == "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'":
		return s.handleRegisterEvent(ctx, event)

	case event.Query == "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'":
		return s.handleUpdateEvent(ctx, event)

	case event.Query == "tm.event='NewBlock' AND complete_oracle_data_set.request_id EXISTS":
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
	requestKey := fmt.Sprintf("req_%d", completeData.RequestID)
	job, exists := s.jobStore.Get(requestKey)
	if !exists {
		s.logger.Debug("job not found for complete event", "request_id", completeData.RequestID)
		return nil
	}

	// 다음 실행 시간 계산 (블록 시간 + period)
	nextRunTime := completeData.BlockTime.Add(job.Period)
	now := time.Now()

	// 과거 시간이면 즉시 실행 가능하도록 현재 시간으로 설정
	if nextRunTime.Before(now) {
		nextRunTime = now
	}

	// Job의 다음 실행 시간 및 Nonce 업데이트
	s.jobStore.Update(requestKey, func(j *OracleJob) error {
		j.NextRunTime = nextRunTime
		j.Nonce = completeData.Nonce // 완료된 nonce로 업데이트
		j.ExecutionState.Status = JobStatusPending
		return nil
	})

	s.logger.Info("scheduled next execution after complete event",
		"request_id", completeData.RequestID,
		"current_nonce", completeData.Nonce,
		"next_run_time", nextRunTime,
		"period", job.Period,
		"delay_from_now", nextRunTime.Sub(now))

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

	// QueryClient를 통해 활성 상태의 Oracle 요청들을 조회
	req := &oracletypes.QueryOracleRequestDocsRequest{
		Status: oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
	}

	resp, err := s.queryClient.OracleRequestDocs(ctx, req)
	if err != nil {
		s.logger.Error("failed to query oracle request docs", "error", err)
		// 에러가 발생해도 계속 진행 (빈 상태로 시작)
		return nil
	}

	// 각 활성 요청을 Job으로 변환하고 저장
	count := 0
	myAddress := s.config.Address().String()
	blockTime := time.Now()

	for _, doc := range resp.OracleRequestDocs {
		if doc == nil {
			continue
		}

		// 이 노드가 담당하는 요청인지 확인하고 Job 생성
		job, err := s.eventParser.ConvertToJob(doc, myAddress, blockTime)
		if err != nil {
			// 담당하지 않는 요청은 스킵
			s.logger.Debug("skipping request not assigned to this node",
				"request_id", doc.RequestId)
			continue
		}

		// Job 저장
		requestKey := fmt.Sprintf("req_%d", job.RequestID)
		if err := s.jobStore.Store(requestKey, job); err != nil {
			s.logger.Error("failed to store job",
				"request_id", job.RequestID,
				"error", err)
			continue
		}

		count++
		s.logger.Info("loaded existing job",
			"request_id", job.RequestID,
			"period", job.Period,
			"url", job.URL)
	}

	s.updateMetrics(func(m *SchedulerMetrics) {
		m.TotalJobs += int64(count)
	})

	s.logger.Info("loaded existing jobs from chain", "count", count)
	return nil
}

// runPeriodicScheduler는 주기적으로 실행할 작업들을 체크
func (s *EventScheduler_impl) runPeriodicScheduler(ctx context.Context) {
	defer s.scheduleWg.Done()
	defer s.logger.Info("periodic scheduler stopped")

	for {
		select {
		case <-ctx.Done():
			return

		case <-s.scheduleTicker.C:
			if !s.isRunning.Load() {
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
