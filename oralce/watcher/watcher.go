package watcher

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// EventWatcherImpl는 EventWatcher 인터페이스의 메인 구현체
type EventWatcherImpl struct {
	logger              log.Logger
	config              *config.Config
	client              SubscriptionClient
	subscriptionManager *SubscriptionManagerImpl

	// 채널들
	eventCh chan coretypes.ResultEvent
	errorCh chan error

	// 상태 관리
	isRunning atomic.Bool
	startedAt time.Time
	mu        sync.RWMutex

	// 메트릭
	totalEvents atomic.Int64
	totalErrors atomic.Int64
	lastEventAt atomic.Value // time.Time
	lastErrorAt atomic.Value // time.Time
}

// NewEventWatcher는 새로운 EventWatcher 인스턴스를 생성
func NewEventWatcher(
	logger log.Logger,
	config *config.Config,
	client SubscriptionClient,
) EventWatcher {

	watcherConfig := WatcherConfig{
		ChannelSize:      config.WorkerChannelSize(),
		ErrorChannelSize: 10,
		MaxRetries:       config.RetryMaxAttempts(),
		RetryDelay:       config.RetryMaxDelay(),
		HealthCheckInt:   30 * time.Second,
	}

	eventCh := make(chan coretypes.ResultEvent, watcherConfig.ChannelSize)
	errorCh := make(chan error, watcherConfig.ErrorChannelSize)

	watcher := &EventWatcherImpl{
		logger:    logger,
		config:    config,
		client:    client,
		eventCh:   eventCh,
		errorCh:   errorCh,
		startedAt: time.Now(),
	}

	// 구독 매니저 초기화
	watcher.subscriptionManager = NewSubscriptionManager(
		logger,
		client,
		watcherConfig,
		eventCh,
		errorCh,
	)

	return watcher
}

// Start는 지정된 쿼리들에 대한 이벤트 구독을 시작
func (w *EventWatcherImpl) Start(ctx context.Context, queries []string) error {
	if w.isRunning.Load() {
		return ErrWatcherAlreadyRunning
	}

	if len(queries) == 0 {
		return ErrNoQueries
	}

	// 클라이언트 연결 상태 확인
	if !w.client.IsRunning() {
		return ErrClientNotConnected
	}

	w.mu.Lock()
	w.startedAt = time.Now()
	w.mu.Unlock()

	// 구독 매니저 시작
	w.subscriptionManager.Start(ctx)

	// 각 쿼리에 대한 구독 시작
	var startupErrors []error
	for _, query := range queries {
		if err := w.subscriptionManager.StartSubscription(ctx, query); err != nil {
			w.logger.Error("failed to start subscription during startup",
				"query", query, "error", err)
			startupErrors = append(startupErrors, err)
		}
	}

	// 하나라도 성공한 구독이 있으면 시작을 허용
	statuses := w.subscriptionManager.GetAllStatuses()
	activeCount := 0
	for _, status := range statuses {
		if status.IsActive {
			activeCount++
		}
	}

	if activeCount == 0 {
		w.logger.Error("no subscriptions could be started")
		return ErrClientNotConnected
	}

	w.isRunning.Store(true)

	// 백그라운드 작업들 시작
	go w.runHealthCheck(ctx)
	go w.runMetricsUpdater(ctx)
	go w.handleShutdown(ctx)

	w.logger.Info("event watcher started",
		"active_subscriptions", activeCount,
		"total_queries", len(queries),
		"startup_errors", len(startupErrors))

	if len(startupErrors) > 0 {
		w.logger.Warn("some subscriptions failed to start",
			"failed_count", len(startupErrors))
	}

	return nil
}

// Stop은 모든 구독을 중지하고 리소스를 정리
func (w *EventWatcherImpl) Stop() error {
	if !w.isRunning.CompareAndSwap(true, false) {
		return ErrWatcherNotRunning
	}

	w.logger.Info("stopping event watcher...")

	// 구독 매니저 중지
	w.subscriptionManager.Stop()

	// 채널 정리
	close(w.eventCh)
	close(w.errorCh)

	w.logger.Info("event watcher stopped",
		"total_events", w.totalEvents.Load(),
		"total_errors", w.totalErrors.Load(),
		"uptime", time.Since(w.startedAt))

	return nil
}

// EventCh는 수신된 이벤트를 전달하는 읽기 전용 채널을 반환
func (w *EventWatcherImpl) EventCh() <-chan coretypes.ResultEvent {
	return w.eventCh
}

// ErrorCh는 발생한 에러를 전달하는 읽기 전용 채널을 반환
func (w *EventWatcherImpl) ErrorCh() <-chan error {
	return w.errorCh
}

// GetStatus는 모든 구독의 현재 상태를 반환
func (w *EventWatcherImpl) GetStatus() map[string]SubscriptionStatus {
	return w.subscriptionManager.GetAllStatuses()
}

// IsRunning은 Watcher의 실행 상태를 반환
func (w *EventWatcherImpl) IsRunning() bool {
	return w.isRunning.Load()
}

// GetMetrics는 현재 메트릭을 반환
func (w *EventWatcherImpl) GetMetrics() WatcherMetrics {
	metrics := w.subscriptionManager.GetMetrics()

	// 로컬 메트릭 추가
	metrics.TotalEvents = w.totalEvents.Load()
	metrics.TotalErrors = w.totalErrors.Load()

	if lastEvent := w.lastEventAt.Load(); lastEvent != nil {
		metrics.LastEventAt = lastEvent.(time.Time)
	}

	if lastError := w.lastErrorAt.Load(); lastError != nil {
		metrics.LastErrorAt = lastError.(time.Time)
	}

	return metrics
}

// AddSubscription은 새로운 구독을 동적으로 추가
func (w *EventWatcherImpl) AddSubscription(ctx context.Context, query string) error {
	if !w.isRunning.Load() {
		return ErrWatcherNotRunning
	}

	return w.subscriptionManager.StartSubscription(ctx, query)
}

// RemoveSubscription은 기존 구독을 제거
func (w *EventWatcherImpl) RemoveSubscription(query string) error {
	if !w.isRunning.Load() {
		return ErrWatcherNotRunning
	}

	return w.subscriptionManager.StopSubscription(query)
}

// RestartSubscription은 특정 구독을 재시작
func (w *EventWatcherImpl) RestartSubscription(ctx context.Context, query string) error {
	if !w.isRunning.Load() {
		return ErrWatcherNotRunning
	}

	return w.subscriptionManager.RestartSubscription(ctx, query)
}

// runHealthCheck는 주기적으로 헬스체크를 수행
func (w *EventWatcherImpl) runHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("health check stopped")
			return
		case <-ticker.C:
			w.performHealthCheck()
		}
	}
}

// performHealthCheck는 실제 헬스체크를 수행
func (w *EventWatcherImpl) performHealthCheck() {
	// 클라이언트 상태 확인
	if !w.client.IsRunning() {
		w.reportHealthIssue("subscription client is not running")
		return
	}

	// 활성 구독 상태 확인
	statuses := w.GetStatus()
	activeCount := 0
	problematicSubs := []string{}

	for query, status := range statuses {
		if status.IsActive {
			activeCount++
			// 오래된 이벤트 체크 (5분 이상 이벤트가 없으면 의심)
			if time.Since(status.LastEvent) > 5*time.Minute {
				problematicSubs = append(problematicSubs, query)
			}
		}
	}

	if activeCount == 0 {
		w.reportHealthIssue("no active subscriptions")
		return
	}

	if len(problematicSubs) > 0 {
		w.logger.Warn("subscriptions with no recent events",
			"queries", problematicSubs)
	}

	w.logger.Debug("health check passed",
		"active_subscriptions", activeCount,
		"problematic_subscriptions", len(problematicSubs))
}

// reportHealthIssue는 헬스체크 이슈를 보고
func (w *EventWatcherImpl) reportHealthIssue(message string) {
	w.logger.Error("health check failed", "issue", message)

	// 에러 채널로 전송 (논블로킹)
	select {
	case w.errorCh <- &EventProcessingError{
		Query:     "health_check",
		EventType: "health",
		Message:   message,
		Fatal:     true,
	}:
	default:
		w.logger.Warn("error channel full, cannot report health issue")
	}
}

// runMetricsUpdater는 주기적으로 메트릭을 업데이트
func (w *EventWatcherImpl) runMetricsUpdater(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("metrics updater stopped")
			return
		case <-ticker.C:
			w.updateMetrics()
		}
	}
}

// updateMetrics는 실시간 메트릭을 업데이트
func (w *EventWatcherImpl) updateMetrics() {
	// 구독별 이벤트 수 집계
	statuses := w.GetStatus()
	totalEvents := int64(0)
	for _, status := range statuses {
		totalEvents += status.EventCount
	}

	w.totalEvents.Store(totalEvents)

	w.logger.Debug("metrics updated",
		"total_events", totalEvents,
		"active_subscriptions", len(statuses))
}

// handleShutdown은 컨텍스트 취소 시 정리 작업을 수행
func (w *EventWatcherImpl) handleShutdown(ctx context.Context) {
	<-ctx.Done()
	w.logger.Info("shutdown signal received")

	// 자동으로 정리
	if w.isRunning.Load() {
		w.Stop()
	}
}

// SubscriptionClientHTTP는 CometBFT HTTP 클라이언트를 래핑하는 어댑터
type SubscriptionClientHTTP struct {
	client interface {
		Subscribe(ctx context.Context, subscriber, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error)
		UnsubscribeAll(ctx context.Context, subscriber string) error
		IsRunning() bool
		Stop() error
	}
}

// NewSubscriptionClientHTTP는 HTTP 클라이언트 어댑터를 생성
func NewSubscriptionClientHTTP(client interface {
	Subscribe(ctx context.Context, subscriber, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error)
	UnsubscribeAll(ctx context.Context, subscriber string) error
	IsRunning() bool
	Stop() error
}) SubscriptionClient {
	return &SubscriptionClientHTTP{client: client}
}

// Subscribe는 구독을 시작
func (c *SubscriptionClientHTTP) Subscribe(ctx context.Context, subscriber, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error) {
	return c.client.Subscribe(ctx, subscriber, query, outCapacity...)
}

// UnsubscribeAll은 모든 구독을 해제
func (c *SubscriptionClientHTTP) UnsubscribeAll(ctx context.Context, subscriber string) error {
	return c.client.UnsubscribeAll(ctx, subscriber)
}

// IsRunning은 클라이언트 실행 상태를 확인
func (c *SubscriptionClientHTTP) IsRunning() bool {
	return c.client.IsRunning()
}

// Stop은 클라이언트를 중지
func (c *SubscriptionClientHTTP) Stop() error {
	return c.client.Stop()
}
