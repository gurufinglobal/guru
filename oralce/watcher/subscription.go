package watcher

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// SubscriptionManagerImpl는 구독 생명주기를 관리하는 구현체
type SubscriptionManagerImpl struct {
	logger          log.Logger
	client          SubscriptionClient
	backoffStrategy BackoffStrategy
	validator       EventValidator

	// 구독 상태 관리
	subscriptions map[string]*SubscriptionStateImpl
	mu            sync.RWMutex

	// 설정
	config WatcherConfig

	// 채널들
	eventCh     chan coretypes.ResultEvent
	errorCh     chan error
	reconnectCh chan string

	// 메트릭
	metrics   *WatcherMetrics
	isRunning atomic.Bool
}

// SubscriptionStateImpl는 개별 구독의 상태를 관리하는 내부 구조체
type SubscriptionStateImpl struct {
	Query           string
	IsActive        bool
	LastEvent       time.Time
	ErrorCount      int32
	LastError       error
	CreatedAt       time.Time
	LastReconnectAt time.Time
	EventCount      int64

	// 내부 관리 정보
	cancel        context.CancelFunc
	eventChannel  <-chan coretypes.ResultEvent
	retryAttempts int
	lastAttemptAt time.Time
}

// NewSubscriptionManager는 새로운 구독 매니저를 생성
func NewSubscriptionManager(
	logger log.Logger,
	client SubscriptionClient,
	config WatcherConfig,
	eventCh chan coretypes.ResultEvent,
	errorCh chan error,
) *SubscriptionManagerImpl {

	backoffStrategy := NewDefaultExponentialBackoff()
	validator := NewDefaultEventValidator()

	return &SubscriptionManagerImpl{
		logger:          logger,
		client:          client,
		backoffStrategy: backoffStrategy,
		validator:       validator,
		subscriptions:   make(map[string]*SubscriptionStateImpl),
		config:          config,
		eventCh:         eventCh,
		errorCh:         errorCh,
		reconnectCh:     make(chan string, 100),
		metrics: &WatcherMetrics{
			BaseMetrics: types.BaseMetrics{
				StartTime: time.Now(),
				StartedAt: time.Now(),
			},
		},
	}
}

// Start는 재연결 처리 고루틴을 시작
func (sm *SubscriptionManagerImpl) Start(ctx context.Context) {
	sm.isRunning.Store(true)
	go sm.handleReconnections(ctx)
}

// StartSubscription은 단일 쿼리에 대한 구독을 시작
func (sm *SubscriptionManagerImpl) StartSubscription(ctx context.Context, query string) error {
	if !sm.isRunning.Load() {
		return ErrWatcherNotRunning
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 이미 존재하는 구독인지 확인
	if existing, exists := sm.subscriptions[query]; exists && existing.IsActive {
		sm.logger.Debug("subscription already active", "query", query)
		return nil
	}

	// 새로운 구독 상태 생성
	state := &SubscriptionStateImpl{
		Query:     query,
		IsActive:  false,
		CreatedAt: time.Now(),
	}

	// 구독 시작
	if err := sm.startSingleSubscription(ctx, state); err != nil {
		sm.logger.Error("failed to start subscription",
			"query", query, "error", err)

		// 에러 채널로 전송 (논블로킹)
		select {
		case sm.errorCh <- WrapError(err, query, "subscription"):
		default:
			sm.logger.Warn("error channel full, dropping error", "query", query)
		}

		return err
	}

	sm.subscriptions[query] = state
	atomic.AddInt32(&sm.metrics.ActiveSubs, 1)

	sm.logger.Info("subscription started", "query", query)
	return nil
}

// startSingleSubscription은 실제 구독을 시작하는 내부 메서드
func (sm *SubscriptionManagerImpl) startSingleSubscription(ctx context.Context, state *SubscriptionStateImpl) error {
	// 컨텍스트 취소 가능하도록 설정
	subCtx, cancel := context.WithCancel(ctx)
	state.cancel = cancel

	// 클라이언트를 통해 구독 시작
	eventCh, err := sm.client.Subscribe(subCtx, "", state.Query, sm.config.ChannelSize)
	if err != nil {
		cancel()
		return NewSubscriptionError(state.Query, "subscription failed", err, true)
	}

	state.eventChannel = eventCh
	state.IsActive = true
	state.LastEvent = time.Now()
	atomic.StoreInt32(&state.ErrorCount, 0)

	// 이벤트 처리 고루틴 시작
	go sm.processSubscriptionEvents(subCtx, state)

	return nil
}

// processSubscriptionEvents는 구독의 이벤트를 처리
func (sm *SubscriptionManagerImpl) processSubscriptionEvents(ctx context.Context, state *SubscriptionStateImpl) {
	defer func() {
		sm.mu.Lock()
		state.IsActive = false
		sm.mu.Unlock()

		atomic.AddInt32(&sm.metrics.ActiveSubs, -1)
		sm.logger.Debug("subscription event processing stopped", "query", state.Query)
	}()

	for {
		select {
		case <-ctx.Done():
			sm.logger.Debug("subscription context cancelled", "query", state.Query)
			return

		case event, ok := <-state.eventChannel:
			if !ok {
				sm.logger.Warn("subscription channel closed", "query", state.Query)
				// 재연결 요청
				sm.scheduleReconnection(state.Query)
				return
			}

			// 이벤트 처리
			if err := sm.processEvent(state, event); err != nil {
				sm.logger.Error("event processing failed",
					"query", state.Query, "error", err)

				atomic.AddInt32(&state.ErrorCount, 1)
				state.LastError = err
				sm.metrics.TotalErrors++
				sm.metrics.LastErrorAt = time.Now()

				// 치명적 에러인 경우 구독 중지
				if IsFatalError(err) {
					sm.logger.Error("fatal error in subscription",
						"query", state.Query, "error", err)
					return
				}
			}
		}
	}
}

// processEvent는 개별 이벤트를 처리
func (sm *SubscriptionManagerImpl) processEvent(state *SubscriptionStateImpl, event coretypes.ResultEvent) error {
	// 이벤트 검증
	if err := sm.validator.Validate(event); err != nil {
		return NewValidationError("event", event.Query, err.Error())
	}

	// 이벤트 처리 여부 확인
	if !sm.validator.ShouldProcess(event) {
		sm.logger.Debug("event filtered out", "query", state.Query)
		return nil
	}

	// 상태 업데이트
	state.LastEvent = time.Now()
	atomic.AddInt64(&state.EventCount, 1)
	atomic.AddInt64(&sm.metrics.TotalEvents, 1)
	sm.metrics.LastEventAt = time.Now()

	// 이벤트 전달 (논블로킹)
	select {
	case sm.eventCh <- event:
		sm.logger.Debug("event forwarded",
			"query", state.Query,
			"event_type", event.Query)
		return nil
	default:
		// 채널이 가득 찬 경우
		sm.logger.Warn("event channel full, dropping event",
			"query", state.Query)
		return NewEventProcessingError(
			state.Query,
			event.Query,
			"event channel full",
			nil,
			false,
		)
	}
}

// StopSubscription은 특정 구독을 중지
func (sm *SubscriptionManagerImpl) StopSubscription(query string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.subscriptions[query]
	if !exists {
		return ErrSubscriptionNotFound
	}

	// 구독 취소
	if state.cancel != nil {
		state.cancel()
	}

	state.IsActive = false
	delete(sm.subscriptions, query)

	sm.logger.Info("subscription stopped", "query", query)
	return nil
}

// RestartSubscription은 구독을 재시작
func (sm *SubscriptionManagerImpl) RestartSubscription(ctx context.Context, query string) error {
	// 기존 구독 중지
	if err := sm.StopSubscription(query); err != nil && err != ErrSubscriptionNotFound {
		return err
	}

	// 새로운 구독 시작
	return sm.StartSubscription(ctx, query)
}

// GetSubscriptionStatus는 특정 구독의 상태를 반환
func (sm *SubscriptionManagerImpl) GetSubscriptionStatus(query string) (SubscriptionStatus, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.subscriptions[query]
	if !exists {
		return SubscriptionStatus{}, false
	}

	return SubscriptionStatus{
		Query:           state.Query,
		IsActive:        state.IsActive,
		LastEvent:       state.LastEvent,
		ErrorCount:      atomic.LoadInt32(&state.ErrorCount),
		LastError:       state.LastError,
		CreatedAt:       state.CreatedAt,
		LastReconnectAt: state.LastReconnectAt,
		EventCount:      atomic.LoadInt64(&state.EventCount),
	}, true
}

// GetAllStatuses는 모든 구독의 상태를 반환
func (sm *SubscriptionManagerImpl) GetAllStatuses() map[string]SubscriptionStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	statuses := make(map[string]SubscriptionStatus)
	for query, state := range sm.subscriptions {
		statuses[query] = SubscriptionStatus{
			Query:           state.Query,
			IsActive:        state.IsActive,
			LastEvent:       state.LastEvent,
			ErrorCount:      atomic.LoadInt32(&state.ErrorCount),
			LastError:       state.LastError,
			CreatedAt:       state.CreatedAt,
			LastReconnectAt: state.LastReconnectAt,
			EventCount:      atomic.LoadInt64(&state.EventCount),
		}
	}

	return statuses
}

// scheduleReconnection은 재연결을 스케줄
func (sm *SubscriptionManagerImpl) scheduleReconnection(query string) {
	select {
	case sm.reconnectCh <- query:
		sm.logger.Debug("reconnection scheduled", "query", query)
	default:
		sm.logger.Warn("reconnection channel full", "query", query)
	}
}

// handleReconnections은 재연결 요청을 처리
func (sm *SubscriptionManagerImpl) handleReconnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			sm.logger.Debug("reconnection handler stopped")
			return

		case query := <-sm.reconnectCh:
			go sm.attemptReconnection(ctx, query)
		}
	}
}

// attemptReconnection은 실제 재연결을 시도
func (sm *SubscriptionManagerImpl) attemptReconnection(ctx context.Context, query string) {
	sm.mu.Lock()
	state, exists := sm.subscriptions[query]
	if !exists || state.IsActive {
		sm.mu.Unlock()
		return
	}

	state.retryAttempts++
	state.lastAttemptAt = time.Now()
	sm.mu.Unlock()

	// 최대 재시도 횟수 확인
	if sm.config.MaxRetries > 0 && state.retryAttempts > sm.config.MaxRetries {
		sm.logger.Error("max retry attempts exceeded",
			"query", query,
			"attempts", state.retryAttempts)

		// 에러 채널로 전송
		select {
		case sm.errorCh <- NewReconnectError(query, state.retryAttempts, sm.config.MaxRetries, ErrMaxRetriesExceeded):
		default:
		}
		return
	}

	// 백오프 지연 적용
	delay := sm.backoffStrategy.Next(state.retryAttempts - 1)
	sm.logger.Info("attempting reconnection",
		"query", query,
		"attempt", state.retryAttempts,
		"delay", delay)

	// 지연 후 재연결 시도
	select {
	case <-time.After(delay):
		if err := sm.startSingleSubscription(ctx, state); err != nil {
			sm.logger.Error("reconnection failed",
				"query", query,
				"attempt", state.retryAttempts,
				"error", err)

			// 재귀적으로 다시 시도 스케줄
			sm.scheduleReconnection(query)
		} else {
			sm.logger.Info("reconnection successful",
				"query", query,
				"attempt", state.retryAttempts)

			// 성공 시 재시도 카운터 리셋
			sm.mu.Lock()
			state.retryAttempts = 0
			state.LastReconnectAt = time.Now()
			sm.mu.Unlock()

			atomic.AddInt64(&sm.metrics.TotalReconnects, 1)
		}

	case <-ctx.Done():
		sm.logger.Debug("reconnection cancelled", "query", query)
		return
	}
}

// Stop은 모든 구독을 중지
func (sm *SubscriptionManagerImpl) Stop() {
	sm.isRunning.Store(false)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 모든 구독 취소
	for query, state := range sm.subscriptions {
		if state.cancel != nil {
			state.cancel()
		}
		sm.logger.Debug("subscription cancelled", "query", query)
	}

	// 리소스 정리
	sm.subscriptions = make(map[string]*SubscriptionStateImpl)
	close(sm.reconnectCh)

	sm.logger.Info("subscription manager stopped")
}

// GetMetrics는 현재 메트릭을 반환
func (sm *SubscriptionManagerImpl) GetMetrics() WatcherMetrics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	metrics := *sm.metrics
	metrics.ActiveSubs = atomic.LoadInt32(&sm.metrics.ActiveSubs)

	// 분당 평균 이벤트 수 계산
	if uptime := time.Since(metrics.StartedAt); uptime > 0 {
		minutes := uptime.Minutes()
		if minutes > 0 {
			metrics.AvgEventsPerMin = float64(metrics.TotalEvents) / minutes
		}
	}

	return metrics
}

// DefaultEventValidator는 기본 이벤트 검증기를 구현
type DefaultEventValidator struct{}

// NewDefaultEventValidator는 기본 이벤트 검증기를 생성
func NewDefaultEventValidator() EventValidator {
	return &DefaultEventValidator{}
}

// Validate는 이벤트의 유효성을 검증
func (v *DefaultEventValidator) Validate(event coretypes.ResultEvent) error {
	if event.Query == "" {
		return NewValidationError("query", "", "event query is empty")
	}

	if event.Data == nil {
		return NewValidationError("data", "", "event data is nil")
	}

	if event.Events == nil {
		return NewValidationError("events", "", "event.Events is nil")
	}

	return nil
}

// ShouldProcess는 이벤트를 처리해야 하는지 결정
func (v *DefaultEventValidator) ShouldProcess(event coretypes.ResultEvent) bool {
	// 기본적으로 모든 유효한 이벤트를 처리
	return v.Validate(event) == nil
}
