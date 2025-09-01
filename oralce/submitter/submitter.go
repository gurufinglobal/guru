package submitter

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/scheduler"
	commontypes "github.com/GPTx-global/guru-v2/oralce/types"
	"github.com/cosmos/cosmos-sdk/client"
)

// JobResultSubmitter_impl는 Oracle 작업 결과 서브미터의 메인 구현체
type JobResultSubmitter_impl struct {
	logger    log.Logger
	config    *config.Config
	clientCtx ClientContext

	// 컴포넌트들
	txBuilder       TransactionBuilder
	sequenceManager SequenceManager
	retryStrategy   RetryStrategy

	// 상태 관리
	isRunning atomic.Bool
	startedAt time.Time

	// 메트릭
	metrics   SubmitterMetrics
	metricsMu sync.RWMutex

	// 동시성 제어
	submitSemaphore chan struct{}
	wg              sync.WaitGroup

	// 타이머
	metricsTicker *time.Ticker
	syncTicker    *time.Ticker
}

// NewJobResultSubmitter는 새로운 Oracle 결과 서브미터를 생성
func NewJobResultSubmitter(
	logger log.Logger,
	cfg *config.Config,
	clientCtx client.Context,
) (JobResultSubmitter, error) {

	// 클라이언트 컨텍스트 어댑터 생성
	clientAdapter := NewDefaultClientContext(clientCtx)

	// 시퀀스 관리자 생성
	sequenceManager, err := NewSequenceManager(
		logger,
		clientAdapter,
		5*time.Minute, // 5분마다 자동 동기화
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create sequence manager: %w", err)
	}

	// 트랜잭션 빌더 생성
	txBuilder, err := NewTransactionBuilder(
		logger,
		cfg,
		clientAdapter,
		sequenceManager,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction builder: %w", err)
	}

	// 재시도 전략 생성
	retryStrategy := NewDefaultRetryStrategy()

	submitter := &JobResultSubmitter_impl{
		logger:          logger,
		config:          cfg,
		clientCtx:       clientAdapter,
		txBuilder:       txBuilder,
		sequenceManager: sequenceManager,
		retryStrategy:   retryStrategy,
		submitSemaphore: make(chan struct{}, cfg.WorkerPoolSize()), // 동시 제출 제한
		metrics: SubmitterMetrics{
			BaseMetrics: commontypes.BaseMetrics{
				StartTime: time.Now(),
				StartedAt: time.Now(),
			},
		},
	}

	return submitter, nil
}

// Start는 서브미터를 시작
func (s *JobResultSubmitter_impl) Start(ctx context.Context) error {
	if s.isRunning.Load() {
		return fmt.Errorf("submitter is already running")
	}

	s.startedAt = time.Now()
	s.metricsMu.Lock()
	s.metrics.StartedAt = s.startedAt
	s.metricsMu.Unlock()

	s.isRunning.Store(true)

	// 백그라운드 작업들 시작
	s.metricsTicker = time.NewTicker(30 * time.Second) // 30초마다 메트릭 업데이트
	s.syncTicker = time.NewTicker(5 * time.Minute)     // 5분마다 시퀀스 동기화

	s.wg.Add(3)
	go s.runMetricsUpdater(ctx)
	go s.runSequenceSync(ctx)
	go s.handleShutdown(ctx)

	s.logger.Info("job result submitter started",
		"max_concurrent", cap(s.submitSemaphore))

	return nil
}

// Stop은 서브미터를 중지하고 리소스를 정리
func (s *JobResultSubmitter_impl) Stop() error {
	if !s.isRunning.CompareAndSwap(true, false) {
		return fmt.Errorf("submitter is not running")
	}

	s.logger.Info("stopping job result submitter...")

	// 타이머 정지
	if s.metricsTicker != nil {
		s.metricsTicker.Stop()
	}
	if s.syncTicker != nil {
		s.syncTicker.Stop()
	}

	// 진행 중인 작업 완료 대기
	s.wg.Wait()

	// 세마포어 정리
	close(s.submitSemaphore)

	s.logger.Info("job result submitter stopped",
		"uptime", time.Since(s.startedAt),
		"total_submissions", s.metrics.TotalSubmissions,
		"success_rate", s.metrics.SuccessRate)

	return nil
}

// Submit은 단일 작업 결과를 제출
func (s *JobResultSubmitter_impl) Submit(ctx context.Context, result *scheduler.JobResult) error {
	if !s.isRunning.Load() {
		return fmt.Errorf("submitter is not running")
	}

	if result == nil {
		return fmt.Errorf("job result cannot be nil")
	}

	// 동시성 제한
	select {
	case s.submitSemaphore <- struct{}{}:
		defer func() { <-s.submitSemaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}

	startTime := time.Now()
	s.updateMetrics(func(m *SubmitterMetrics) {
		m.TotalSubmissions++
		m.PendingSubmits++
	})

	s.logger.Debug("submitting job result",
		"job_id", result.JobID,
		"request_id", result.RequestID,
		"nonce", result.Nonce)

	// 재시도 로직으로 제출 실행
	submitResult := s.submitWithRetry(ctx, result)

	// 메트릭 업데이트
	duration := time.Since(startTime)
	s.updateSubmissionMetrics(submitResult, duration)

	if !submitResult.Success {
		s.logger.Error("failed to submit job result",
			"job_id", result.JobID,
			"attempts", submitResult.Attempts,
			"duration", duration,
			"error", submitResult.Error)
		return submitResult.Error
	}

	s.logger.Info("job result submitted successfully",
		"job_id", result.JobID,
		"tx_hash", submitResult.TxHash,
		"attempts", submitResult.Attempts,
		"duration", duration,
		"gas_used", submitResult.GasUsed)

	return nil
}

// SubmitBatch는 여러 작업 결과를 일괄 제출
func (s *JobResultSubmitter_impl) SubmitBatch(ctx context.Context, results []*scheduler.JobResult) error {
	if !s.isRunning.Load() {
		return fmt.Errorf("submitter is not running")
	}

	if len(results) == 0 {
		return fmt.Errorf("no results to submit")
	}

	// 유효한 결과만 필터링
	validResults := make([]*scheduler.JobResult, 0, len(results))
	for _, result := range results {
		if result != nil && result.Success {
			validResults = append(validResults, result)
		}
	}

	if len(validResults) == 0 {
		return fmt.Errorf("no valid results to submit")
	}

	s.logger.Info("submitting batch results",
		"total_results", len(results),
		"valid_results", len(validResults))

	// 배치 크기 제한 확인
	maxBatchSize := s.config.WorkerPoolSize() // 워커 풀 크기를 배치 크기로 사용

	if len(validResults) > maxBatchSize {
		// 큰 배치를 작은 청크로 분할
		return s.submitInChunks(ctx, validResults, maxBatchSize)
	}

	// 작은 배치는 한 번에 제출
	return s.submitSingleBatch(ctx, validResults)
}

// submitWithRetry는 재시도 로직으로 결과를 제출
func (s *JobResultSubmitter_impl) submitWithRetry(ctx context.Context, result *scheduler.JobResult) *SubmitResult {
	var lastErr error
	var txHash string
	startTime := time.Now()

	for attempt := 0; attempt <= s.retryStrategy.GetMaxRetries(); attempt++ {
		if attempt > 0 {
			// 재시도 지연
			delay := s.retryStrategy.GetDelay(attempt - 1)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return &SubmitResult{
					JobID:    result.JobID,
					Success:  false,
					Error:    ctx.Err(),
					Attempts: attempt + 1,
					Duration: time.Since(startTime),
				}
			}
		}

		// 트랜잭션 빌드
		txBytes, err := s.txBuilder.BuildSubmitTx(ctx, result)
		if err != nil {
			lastErr = err
			s.logger.Error("failed to build transaction",
				"job_id", result.JobID,
				"attempt", attempt+1,
				"error", err)
			continue
		}

		// 트랜잭션 브로드캐스트
		txResponse, err := s.clientCtx.BroadcastTx(txBytes)
		if err != nil {
			lastErr = err
			s.logger.Error("failed to broadcast transaction",
				"job_id", result.JobID,
				"attempt", attempt+1,
				"error", err)

			// 네트워크 에러인 경우 재시도
			if s.retryStrategy.ShouldRetry(err, attempt) {
				continue
			} else {
				break
			}
		}

		// 트랜잭션 결과 처리
		txHash = txResponse.TxHash

		if txResponse.Code == 0 {
			// 성공
			s.sequenceManager.NextSequence() // 시퀀스 증가

			return &SubmitResult{
				JobID:       result.JobID,
				TxHash:      txHash,
				Success:     true,
				Attempts:    attempt + 1,
				Duration:    time.Since(startTime),
				GasUsed:     uint64(txResponse.GasUsed),
				SubmittedAt: time.Now(),
			}
		}

		// 트랜잭션 에러 처리
		txErr := &TransactionError{
			JobID:     result.JobID,
			TxHash:    txHash,
			Code:      txResponse.Code,
			Codespace: txResponse.Codespace,
			Message:   txResponse.RawLog,
			RawLog:    txResponse.RawLog,
			GasUsed:   uint64(txResponse.GasUsed),
			GasWanted: uint64(txResponse.GasWanted),
			Retryable: s.isRetryableCode(txResponse.Code),
		}

		lastErr = txErr

		// 시퀀스 에러 처리
		if err := s.handleTransactionError(ctx, txErr); err != nil {
			s.logger.Error("failed to handle transaction error",
				"job_id", result.JobID,
				"tx_hash", txHash,
				"error", err)
		}

		// 재시도 가능성 확인
		if !s.retryStrategy.ShouldRetry(txErr, attempt) {
			break
		}

		s.logger.Warn("transaction failed, retrying",
			"job_id", result.JobID,
			"tx_hash", txHash,
			"code", txResponse.Code,
			"attempt", attempt+1,
			"max_attempts", s.retryStrategy.GetMaxRetries()+1)
	}

	// 모든 재시도 실패
	return &SubmitResult{
		JobID:    result.JobID,
		TxHash:   txHash,
		Success:  false,
		Error:    lastErr,
		Attempts: s.retryStrategy.GetMaxRetries() + 1,
		Duration: time.Since(startTime),
	}
}

// submitSingleBatch는 단일 배치를 제출
func (s *JobResultSubmitter_impl) submitSingleBatch(ctx context.Context, results []*scheduler.JobResult) error {
	// 배치 트랜잭션 빌드
	txBytes, err := s.txBuilder.(*TransactionBuilder_impl).BuildBatchSubmitTx(ctx, results)
	if err != nil {
		s.logger.Error("failed to build batch transaction", "error", err)
		return err
	}

	// 트랜잭션 브로드캐스트
	txResponse, err := s.clientCtx.BroadcastTx(txBytes)
	if err != nil {
		s.logger.Error("failed to broadcast batch transaction", "error", err)
		return err
	}

	if txResponse.Code != 0 {
		err := fmt.Errorf("batch transaction failed: code=%d, log=%s",
			txResponse.Code, txResponse.RawLog)
		s.logger.Error("batch transaction failed",
			"code", txResponse.Code,
			"raw_log", txResponse.RawLog)
		return err
	}

	// 성공 시 시퀀스 증가
	s.sequenceManager.NextSequence()

	s.logger.Info("batch submitted successfully",
		"tx_hash", txResponse.TxHash,
		"batch_size", len(results),
		"gas_used", txResponse.GasUsed)

	// 메트릭 업데이트
	s.updateMetrics(func(m *SubmitterMetrics) {
		m.TotalSubmissions += int64(len(results))
		m.SuccessfulSubmits += int64(len(results))
		m.LastSubmissionAt = time.Now()
	})

	return nil
}

// submitInChunks는 큰 배치를 청크로 나누어 제출
func (s *JobResultSubmitter_impl) submitInChunks(ctx context.Context, results []*scheduler.JobResult, chunkSize int) error {
	var errors []error

	for i := 0; i < len(results); i += chunkSize {
		end := i + chunkSize
		if end > len(results) {
			end = len(results)
		}

		chunk := results[i:end]
		if err := s.submitSingleBatch(ctx, chunk); err != nil {
			errors = append(errors, err)
			s.logger.Error("chunk submission failed",
				"chunk_start", i,
				"chunk_size", len(chunk),
				"error", err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("batch submission failed: %d chunks failed out of %d",
			len(errors), (len(results)+chunkSize-1)/chunkSize)
	}

	return nil
}

// handleTransactionError는 트랜잭션 에러를 처리
func (s *JobResultSubmitter_impl) handleTransactionError(ctx context.Context, txErr *TransactionError) error {
	switch txErr.Code {
	case 32: // account sequence mismatch
		s.updateMetrics(func(m *SubmitterMetrics) {
			m.SequenceErrors++
		})
		return s.sequenceManager.HandleSequenceError(ctx, txErr)

	case 19: // tx already in mempool
		// 트랜잭션이 이미 메모리풀에 있으면 시퀀스만 증가
		s.sequenceManager.NextSequence()
		return nil

	case 13: // insufficient fee
		s.updateMetrics(func(m *SubmitterMetrics) {
			m.InsufficientFeeErrs++
		})
		// TODO: 가스 가격 조정 로직
		return nil

	default:
		s.updateMetrics(func(m *SubmitterMetrics) {
			m.OtherErrors++
		})
		return nil
	}
}

// isRetryableCode는 트랜잭션 코드가 재시도 가능한지 확인
func (s *JobResultSubmitter_impl) isRetryableCode(code uint32) bool {
	switch code {
	case 32, 19, 11, 13: // sequence mismatch, tx in mempool, out of gas, insufficient fee
		return true
	default:
		return false
	}
}

// GetMetrics는 현재 제출 메트릭을 반환
func (s *JobResultSubmitter_impl) GetMetrics() SubmitterMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	metrics := s.metrics
	metrics.Uptime = time.Since(s.startedAt)

	// 성공률 계산
	total := metrics.SuccessfulSubmits + metrics.FailedSubmits
	if total > 0 {
		metrics.SuccessRate = float64(metrics.SuccessfulSubmits) / float64(total)
	}

	return metrics
}

// IsRunning은 서브미터 실행 상태를 반환
func (s *JobResultSubmitter_impl) IsRunning() bool {
	return s.isRunning.Load()
}

// updateMetrics는 메트릭을 안전하게 업데이트
func (s *JobResultSubmitter_impl) updateMetrics(updater func(*SubmitterMetrics)) {
	s.metricsMu.Lock()
	updater(&s.metrics)
	s.metricsMu.Unlock()
}

// updateSubmissionMetrics는 제출 결과 메트릭을 업데이트
func (s *JobResultSubmitter_impl) updateSubmissionMetrics(result *SubmitResult, duration time.Duration) {
	s.updateMetrics(func(m *SubmitterMetrics) {
		m.PendingSubmits--

		if result.Success {
			m.SuccessfulSubmits++
		} else {
			m.FailedSubmits++
		}

		if result.Attempts > 1 {
			m.TotalRetries += int64(result.Attempts - 1)
		}

		m.LastSubmissionAt = time.Now()

		// 평균 제출 시간 업데이트 (간단한 이동 평균)
		if m.AvgSubmissionTime == 0 {
			m.AvgSubmissionTime = duration
		} else {
			m.AvgSubmissionTime = (m.AvgSubmissionTime + duration) / 2
		}
	})
}

// runMetricsUpdater는 주기적으로 메트릭을 업데이트
func (s *JobResultSubmitter_impl) runMetricsUpdater(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.metricsTicker.C:
			// 추가 메트릭 수집 로직 (필요시)
			s.logger.Debug("metrics updated",
				"total_submissions", s.metrics.TotalSubmissions,
				"success_rate", s.metrics.SuccessRate)
		}
	}
}

// runSequenceSync는 주기적으로 시퀀스를 동기화
func (s *JobResultSubmitter_impl) runSequenceSync(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.syncTicker.C:
			if err := s.sequenceManager.SyncWithChain(ctx); err != nil {
				s.logger.Error("periodic sequence sync failed", "error", err)
			}
		}
	}
}

// handleShutdown은 컨텍스트 취소 시 정리
func (s *JobResultSubmitter_impl) handleShutdown(ctx context.Context) {
	defer s.wg.Done()

	<-ctx.Done()
	s.logger.Info("shutdown signal received")

	if s.isRunning.Load() {
		s.Stop()
	}
}
