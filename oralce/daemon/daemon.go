package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	guruconfig "github.com/GPTx-global/guru-v2/cmd/gurud/config"
	"github.com/GPTx-global/guru-v2/encoding"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/scheduler"
	"github.com/GPTx-global/guru-v2/oralce/submitter"
	commontypes "github.com/GPTx-global/guru-v2/oralce/types"
	"github.com/GPTx-global/guru-v2/oralce/watcher"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	comethttp "github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/rs/zerolog"
)

// OracleDaemon는 Oracle 서비스의 메인 오케스트레이터
type OracleDaemon struct {
	logger log.Logger
	config *config.Config

	// 블록체인 클라이언트
	cometClient *comethttp.HTTP
	clientCtx   client.Context
	queryClient oracletypes.QueryClient

	// 핵심 컴포넌트들
	eventWatcher    watcher.EventWatcher
	jobScheduler    scheduler.EventScheduler
	resultSubmitter submitter.JobResultSubmitter

	// 상태 관리
	isRunning  atomic.Bool
	startedAt  time.Time
	fatalCh    chan error
	shutdownCh chan struct{}

	// 동시성 제어
	wg sync.WaitGroup

	// 메트릭
	metrics   *DaemonMetrics
	metricsMu sync.RWMutex
}

// DaemonMetrics는 데몬 전체 메트릭을 나타내는 구조체
type DaemonMetrics struct {
	commontypes.BaseMetrics

	EventsProcessed  int64     `json:"events_processed"`
	JobsScheduled    int64     `json:"jobs_scheduled"`
	JobsCompleted    int64     `json:"jobs_completed"`
	TxSubmitted      int64     `json:"tx_submitted"`
	FatalErrors      int64     `json:"fatal_errors"`
	LastJobAt        time.Time `json:"last_job_at"`
	LastSubmissionAt time.Time `json:"last_submission_at"`
}

// New는 새로운 Oracle Daemon 인스턴스를 생성
func NewOracleDaemon(ctx context.Context, configPath string) (*OracleDaemon, error) {
	// 설정 로드
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	daemon := &OracleDaemon{
		logger:     log.NewLogger(os.Stdout, log.LevelOption(zerolog.InfoLevel)),
		config:     cfg,
		fatalCh:    make(chan error, 10),
		shutdownCh: make(chan struct{}),
		metrics: &DaemonMetrics{
			BaseMetrics: commontypes.BaseMetrics{
				StartTime:    time.Now(),
				HealthStatus: commontypes.HealthStatusInitializing,
			},
		},
	}

	// 클라이언트 설정
	if err := daemon.setupClients(); err != nil {
		return nil, fmt.Errorf("failed to setup clients: %w", err)
	}

	// 컴포넌트 초기화
	if err := daemon.initializeComponents(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	daemon.logger.Info("oracle daemon initialized",
		"chain_id", cfg.ChainID(),
		"address", cfg.Address().String(),
		"worker_pool_size", cfg.WorkerPoolSize())

	return daemon, nil
}

// setupClients는 블록체인 클라이언트들을 설정
func (d *OracleDaemon) setupClients() error {
	// 인코딩 설정
	encCfg := encoding.MakeConfig(guruconfig.GuruChainID)
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	oracletypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	// CometBFT HTTP 클라이언트 생성
	cometClient, err := comethttp.New(d.config.ChainEndpoint(), "/websocket")
	if err != nil {
		return fmt.Errorf("failed to create comet client: %w", err)
	}

	// 클라이언트 시작
	if err := cometClient.Start(); err != nil {
		return fmt.Errorf("failed to start comet client: %w", err)
	}

	// 클라이언트 컨텍스트 생성
	d.clientCtx = client.Context{}.
		WithCodec(encCfg.Codec).
		WithInterfaceRegistry(encCfg.InterfaceRegistry).
		WithTxConfig(encCfg.TxConfig).
		WithLegacyAmino(encCfg.Amino).
		WithKeyring(d.config.Keyring()).
		WithChainID(d.config.ChainID()).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithNodeURI(d.config.ChainEndpoint()).
		WithClient(cometClient).
		WithFromAddress(d.config.Address()).
		WithFromName(d.config.KeyName()).
		WithBroadcastMode(flags.BroadcastSync)

	d.cometClient = cometClient
	d.queryClient = oracletypes.NewQueryClient(d.clientCtx)

	d.logger.Info("blockchain clients initialized",
		"endpoint", d.config.ChainEndpoint(),
		"chain_id", d.config.ChainID())

	return nil
}

// initializeComponents는 모든 핵심 컴포넌트들을 초기화
func (d *OracleDaemon) initializeComponents(ctx context.Context) error {
	// Watcher 생성
	subscriptionClient := watcher.NewSubscriptionClientHTTP(d.cometClient)
	d.eventWatcher = watcher.NewEventWatcher(d.logger, d.config, subscriptionClient)

	// Scheduler 생성
	queryClientAdapter := &QueryClientAdapter{client: d.queryClient}
	eventParser := scheduler.NewEventParser(d.logger, queryClientAdapter)
	jobExecutor := scheduler.NewJobExecutor(d.logger, d.config)

	resultCh := make(chan *scheduler.JobResult, 100)
	d.jobScheduler = scheduler.NewEventScheduler(eventParser, jobExecutor, *d.config, resultCh, d.logger)

	// Submitter 생성
	resultSubmitter, err := submitter.NewJobResultSubmitter(d.logger, d.config, d.clientCtx)
	if err != nil {
		return fmt.Errorf("failed to create result submitter: %w", err)
	}
	d.resultSubmitter = resultSubmitter

	d.logger.Info("all components initialized")
	return nil
}

// Start는 Oracle Daemon을 시작하고 모든 컴포넌트를 조율
func (d *OracleDaemon) Start(ctx context.Context) error {
	if d.isRunning.Load() {
		return fmt.Errorf("oracle daemon is already running")
	}

	d.startedAt = time.Now()
	d.updateMetrics(func(m *DaemonMetrics) {
		m.StartTime = d.startedAt
		m.HealthStatus = "starting"
	})

	d.logger.Info("starting oracle daemon...")

	// 1. Submitter 시작
	if err := d.resultSubmitter.Start(ctx); err != nil {
		return fmt.Errorf("failed to start result submitter: %w", err)
	}

	// 2. Scheduler 시작
	if err := d.jobScheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start job scheduler: %w", err)
	}

	// 3. Watcher 시작 (이벤트 쿼리 정의)
	eventQueries := []string{
		commontypes.RegisterQuery,
		commontypes.UpdateQuery,
		commontypes.CompleteQuery,
	}

	if err := d.eventWatcher.Start(ctx, eventQueries); err != nil {
		return fmt.Errorf("failed to start event watcher: %w", err)
	}

	d.isRunning.Store(true)
	d.updateMetrics(func(m *DaemonMetrics) {
		m.HealthStatus = "running"
	})

	// 백그라운드 작업들 시작
	d.wg.Add(4)
	go d.runEventProcessor(ctx)
	go d.runResultProcessor(ctx)
	go d.runHealthMonitor(ctx)
	go d.runMetricsCollector(ctx)

	// 종료 처리
	go d.handleShutdown(ctx)

	d.logger.Info("oracle daemon started successfully",
		"uptime", time.Since(d.startedAt))

	return nil
}

// runEventProcessor는 Watcher의 이벤트를 Scheduler로 전달
func (d *OracleDaemon) runEventProcessor(ctx context.Context) {
	defer d.wg.Done()
	defer d.logger.Debug("event processor stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.shutdownCh:
			return

		case event, ok := <-d.eventWatcher.EventCh():
			if !ok {
				d.handleFatalError(fmt.Errorf("event watcher channel closed"))
				return
			}

			// 이벤트 처리
			if err := d.processEvent(ctx, event); err != nil {
				d.logger.Error("failed to process event",
					"query", event.Query,
					"error", err)
				d.updateMetrics(func(m *DaemonMetrics) {
					m.TotalErrors++
				})
			}

		case err, ok := <-d.eventWatcher.ErrorCh():
			if !ok {
				continue
			}

			d.logger.Error("watcher error received", "error", err)
			d.updateMetrics(func(m *DaemonMetrics) {
				m.TotalErrors++
			})

			// 치명적 에러 확인
			if watcher.IsFatalError(err) {
				d.handleFatalError(fmt.Errorf("fatal watcher error: %w", err))
				return
			}
		}
	}
}

// processEvent는 단일 이벤트를 처리
func (d *OracleDaemon) processEvent(ctx context.Context, event coretypes.ResultEvent) error {
	d.logger.Debug("processing event",
		"query", event.Query,
		"height", event.Data)

	// Scheduler로 이벤트 전달
	if err := d.jobScheduler.ProcessEvent(ctx, event); err != nil {
		return fmt.Errorf("scheduler failed to process event: %w", err)
	}

	d.updateMetrics(func(m *DaemonMetrics) {
		m.EventsProcessed++
		m.LastEventAt = time.Now()
	})

	return nil
}

// runResultProcessor는 Scheduler의 결과를 Submitter로 전달
func (d *OracleDaemon) runResultProcessor(ctx context.Context) {
	defer d.wg.Done()
	defer d.logger.Debug("result processor stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.shutdownCh:
			return

		case result, ok := <-d.jobScheduler.GetResultChannel():
			if !ok {
				d.handleFatalError(fmt.Errorf("job scheduler result channel closed"))
				return
			}

			// 결과 처리
			if err := d.processJobResult(ctx, result); err != nil {
				d.logger.Error("failed to process job result",
					"job_id", result.JobID,
					"error", err)
				d.updateMetrics(func(m *DaemonMetrics) {
					m.TotalErrors++
				})
			}
		}
	}
}

// processJobResult는 단일 작업 결과를 처리
func (d *OracleDaemon) processJobResult(ctx context.Context, result *scheduler.JobResult) error {
	if result == nil {
		return fmt.Errorf("job result is nil")
	}

	d.logger.Debug("processing job result",
		"job_id", result.JobID,
		"request_id", result.RequestID,
		"success", result.Success)

	// 실패한 작업은 로깅만 하고 제출하지 않음
	if !result.Success {
		d.logger.Warn("job failed, skipping submission",
			"job_id", result.JobID,
			"error", result.Error)
		return nil
	}

	// Submitter로 결과 전달
	if err := d.resultSubmitter.Submit(ctx, result); err != nil {
		return fmt.Errorf("failed to submit job result: %w", err)
	}

	d.updateMetrics(func(m *DaemonMetrics) {
		m.JobsCompleted++
		m.TxSubmitted++
		m.LastSubmissionAt = time.Now()
	})

	return nil
}

// runHealthMonitor는 주기적으로 시스템 헬스를 모니터링
func (d *OracleDaemon) runHealthMonitor(ctx context.Context) {
	defer d.wg.Done()
	defer d.logger.Debug("health monitor stopped")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.shutdownCh:
			return
		case <-ticker.C:
			d.performHealthCheck()
		}
	}
}

// performHealthCheck는 실제 헬스체크를 수행
func (d *OracleDaemon) performHealthCheck() {
	healthy := true
	issues := []string{}

	// 1. CometBFT 클라이언트 상태 확인
	if !d.cometClient.IsRunning() {
		healthy = false
		issues = append(issues, "comet client not running")
	} else {
		// WebSocket 연결 테스트
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := d.cometClient.Status(ctx)
		cancel()

		if err != nil {
			healthy = false
			issues = append(issues, fmt.Sprintf("comet client status error: %v", err))
		}
	}

	// 2. 컴포넌트 상태 확인
	if !d.eventWatcher.IsRunning() {
		healthy = false
		issues = append(issues, "event watcher not running")
	}

	if !d.jobScheduler.IsRunning() {
		healthy = false
		issues = append(issues, "job scheduler not running")
	}

	if !d.resultSubmitter.IsRunning() {
		healthy = false
		issues = append(issues, "result submitter not running")
	}

	// 3. Watcher 구독 상태 확인
	watcherStatus := d.eventWatcher.GetStatus()
	inactiveSubscriptions := 0
	for query, status := range watcherStatus {
		if !status.IsActive {
			inactiveSubscriptions++
			issues = append(issues, fmt.Sprintf("subscription inactive: %s", query))
		}

		// 오래된 이벤트 확인 (5분 이상 이벤트가 없으면 의심)
		if time.Since(status.LastEvent) > 5*time.Minute {
			issues = append(issues, fmt.Sprintf("no recent events: %s", query))
		}
	}

	if inactiveSubscriptions > len(watcherStatus)/2 {
		healthy = false
		issues = append(issues, "too many inactive subscriptions")
	}

	// 헬스 상태 업데이트
	healthStatus := commontypes.HealthStatusHealthy
	if !healthy {
		healthStatus = commontypes.HealthStatusUnhealthy

		// 치명적 상태인지 확인
		if len(issues) >= 3 || inactiveSubscriptions == len(watcherStatus) {
			d.handleFatalError(fmt.Errorf("critical health issues: %v", issues))
			return
		}
	}

	d.updateMetrics(func(m *DaemonMetrics) {
		m.HealthStatus = healthStatus
	})

	if healthy {
		d.logger.Debug("health check passed",
			"active_subscriptions", len(watcherStatus)-inactiveSubscriptions,
			"total_subscriptions", len(watcherStatus))
	} else {
		d.logger.Warn("health check failed",
			"issues", issues)
	}
}

// runMetricsCollector는 주기적으로 메트릭을 수집하고 집계
func (d *OracleDaemon) runMetricsCollector(ctx context.Context) {
	defer d.wg.Done()
	defer d.logger.Debug("metrics collector stopped")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.shutdownCh:
			return
		case <-ticker.C:
			d.collectMetrics()
		}
	}
}

// collectMetrics는 모든 컴포넌트에서 메트릭을 수집
func (d *OracleDaemon) collectMetrics() {
	// Watcher 메트릭
	watcherMetrics := d.eventWatcher.GetMetrics()

	// Scheduler 메트릭
	schedulerMetrics := d.jobScheduler.GetMetrics()

	// Submitter 메트릭
	submitterMetrics := d.resultSubmitter.GetMetrics()

	// 전체 메트릭 업데이트
	d.updateMetrics(func(m *DaemonMetrics) {
		m.Uptime = time.Since(d.startedAt)
		m.EventsProcessed = watcherMetrics.TotalEvents
		m.JobsScheduled = schedulerMetrics.TotalJobs
		m.JobsCompleted = schedulerMetrics.CompletedJobs
		m.TxSubmitted = submitterMetrics.SuccessfulSubmits
		m.TotalErrors = watcherMetrics.TotalErrors + schedulerMetrics.EventsFailed + submitterMetrics.FailedSubmits

		if !watcherMetrics.LastEventAt.IsZero() {
			m.LastEventAt = watcherMetrics.LastEventAt
		}
		if !schedulerMetrics.LastJobAt.IsZero() {
			m.LastJobAt = schedulerMetrics.LastJobAt
		}
		if !submitterMetrics.LastSubmissionAt.IsZero() {
			m.LastSubmissionAt = submitterMetrics.LastSubmissionAt
		}
	})

	d.logger.Debug("metrics updated",
		"events_processed", d.metrics.EventsProcessed,
		"jobs_completed", d.metrics.JobsCompleted,
		"tx_submitted", d.metrics.TxSubmitted,
		"uptime", d.metrics.Uptime)
}

// Stop은 Oracle Daemon을 우아하게 종료
func (d *OracleDaemon) Stop() error {
	ctx := context.Background()
	if !d.isRunning.CompareAndSwap(true, false) {
		return fmt.Errorf("oracle daemon is not running")
	}

	d.logger.Info("stopping oracle daemon...")

	d.updateMetrics(func(m *DaemonMetrics) {
		m.HealthStatus = "stopping"
	})

	// 종료 신호 전송
	close(d.shutdownCh)

	// 컴포넌트들 순서대로 종료
	if d.eventWatcher != nil {
		if err := d.eventWatcher.Stop(); err != nil {
			d.logger.Error("failed to stop event watcher", "error", err)
		}
	}

	if d.jobScheduler != nil {
		if err := d.jobScheduler.Stop(ctx); err != nil {
			d.logger.Error("failed to stop job scheduler", "error", err)
		}
	}

	if d.resultSubmitter != nil {
		if err := d.resultSubmitter.Stop(); err != nil {
			d.logger.Error("failed to stop result submitter", "error", err)
		}
	}

	// CometBFT 클라이언트 종료
	if d.cometClient != nil && d.cometClient.IsRunning() {
		if err := d.cometClient.Stop(); err != nil {
			d.logger.Error("failed to stop comet client", "error", err)
		}
	}

	// 백그라운드 고루틴 종료 대기
	d.wg.Wait()

	// Fatal 채널 정리
	close(d.fatalCh)

	d.updateMetrics(func(m *DaemonMetrics) {
		m.HealthStatus = "stopped"
		m.Uptime = time.Since(d.startedAt)
	})

	d.logger.Info("oracle daemon stopped",
		"uptime", time.Since(d.startedAt),
		"events_processed", d.metrics.EventsProcessed,
		"jobs_completed", d.metrics.JobsCompleted,
		"tx_submitted", d.metrics.TxSubmitted)

	return nil
}

// GetMetrics는 현재 데몬 메트릭을 반환
func (d *OracleDaemon) GetMetrics() DaemonMetrics {
	d.metricsMu.RLock()
	defer d.metricsMu.RUnlock()

	metrics := *d.metrics
	metrics.Uptime = time.Since(d.startedAt)

	return metrics
}

// Fatal은 치명적 에러를 전달하는 채널을 반환
func (d *OracleDaemon) Fatal() <-chan error {
	return d.fatalCh
}

// IsRunning은 데몬 실행 상태를 반환
func (d *OracleDaemon) IsRunning() bool {
	return d.isRunning.Load()
}

// handleFatalError는 치명적 에러를 처리
func (d *OracleDaemon) handleFatalError(err error) {
	d.logger.Error("fatal error occurred", "error", err)

	d.updateMetrics(func(m *DaemonMetrics) {
		m.FatalErrors++
		m.HealthStatus = "fatal"
	})

	// 논블로킹으로 에러 전송
	select {
	case d.fatalCh <- err:
	default:
		d.logger.Warn("fatal error channel full, error dropped")
	}
}

// handleShutdown은 컨텍스트 취소 시 자동 종료를 처리
func (d *OracleDaemon) handleShutdown(ctx context.Context) {
	<-ctx.Done()
	d.logger.Info("shutdown signal received")

	if d.isRunning.Load() {
		d.Stop()
	}
}

// updateMetrics는 메트릭을 안전하게 업데이트
func (d *OracleDaemon) updateMetrics(updater func(*DaemonMetrics)) {
	d.metricsMu.Lock()
	updater(d.metrics)
	d.metricsMu.Unlock()
}

// QueryClientAdapter는 oracletypes.QueryClient를 scheduler.QueryClient로 어댑팅
type QueryClientAdapter struct {
	client oracletypes.QueryClient
}

// OracleRequestDoc은 특정 요청 문서를 조회
func (q *QueryClientAdapter) OracleRequestDoc(ctx context.Context, req *oracletypes.QueryOracleRequestDocRequest) (*oracletypes.QueryOracleRequestDocResponse, error) {
	return q.client.OracleRequestDoc(ctx, req)
}

// OracleRequestDocs는 요청 문서 목록을 조회
func (q *QueryClientAdapter) OracleRequestDocs(ctx context.Context, req *oracletypes.QueryOracleRequestDocsRequest) (*oracletypes.QueryOracleRequestDocsResponse, error) {
	return q.client.OracleRequestDocs(ctx, req)
}

// OracleData는 Oracle 데이터를 조회
func (q *QueryClientAdapter) OracleData(ctx context.Context, req *oracletypes.QueryOracleDataRequest) (*oracletypes.QueryOracleDataResponse, error) {
	return q.client.OracleData(ctx, req)
}
