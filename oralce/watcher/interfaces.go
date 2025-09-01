package watcher

import (
	"context"
	"time"

	"github.com/GPTx-global/guru-v2/oralce/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// SubscriptionClient는 블록체인 이벤트 구독을 위한 인터페이스
// 테스트 가능성을 위해 실제 CometBFT HTTP 클라이언트를 추상화
type SubscriptionClient interface {
	// Subscribe는 특정 쿼리에 대한 이벤트 구독을 시작
	Subscribe(ctx context.Context, subscriber, query string, outCapacity ...int) (<-chan coretypes.ResultEvent, error)

	// UnsubscribeAll은 모든 활성 구독을 해제
	UnsubscribeAll(ctx context.Context, subscriber string) error

	// IsRunning은 클라이언트의 실행 상태를 확인
	IsRunning() bool

	// Stop은 클라이언트를 중지
	Stop() error
}

// EventWatcher는 블록체인 이벤트 감시 및 전달을 담당하는 인터페이스
type EventWatcher interface {
	// Start는 지정된 쿼리들에 대한 이벤트 구독을 시작
	Start(ctx context.Context, queries []string) error

	// Stop은 모든 구독을 중지하고 리소스를 정리
	Stop() error

	// EventCh는 수신된 이벤트를 전달하는 읽기 전용 채널을 반환
	EventCh() <-chan coretypes.ResultEvent

	// ErrorCh는 발생한 에러를 전달하는 읽기 전용 채널을 반환
	ErrorCh() <-chan error

	// GetStatus는 모든 구독의 현재 상태를 반환
	GetStatus() map[string]SubscriptionStatus

	// IsRunning은 Watcher의 실행 상태를 반환
	IsRunning() bool

	// GetMetrics는 현재 메트릭을 반환
	GetMetrics() WatcherMetrics
}

// SubscriptionManager는 개별 구독의 생명주기를 관리하는 인터페이스
type SubscriptionManager interface {
	// StartSubscription은 단일 쿼리에 대한 구독을 시작
	StartSubscription(ctx context.Context, query string) error

	// StopSubscription은 특정 구독을 중지
	StopSubscription(query string) error

	// RestartSubscription은 구독을 재시작
	RestartSubscription(ctx context.Context, query string) error

	// GetSubscriptionStatus는 특정 구독의 상태를 반환
	GetSubscriptionStatus(query string) (SubscriptionStatus, bool)
}

// BackoffStrategy는 types.BackoffStrategy의 별칭
type BackoffStrategy = types.BackoffStrategy

// EventValidator는 수신된 이벤트의 유효성을 검증하는 인터페이스
type EventValidator interface {
	// Validate는 이벤트의 유효성을 검증
	Validate(event coretypes.ResultEvent) error

	// ShouldProcess는 이벤트를 처리해야 하는지 결정
	ShouldProcess(event coretypes.ResultEvent) bool
}

// SubscriptionStatus는 구독의 현재 상태를 나타내는 구조체
type SubscriptionStatus struct {
	Query           string    `json:"query"`             // 구독 쿼리
	IsActive        bool      `json:"is_active"`         // 활성 상태
	LastEvent       time.Time `json:"last_event"`        // 마지막 이벤트 수신 시간
	ErrorCount      int32     `json:"error_count"`       // 에러 발생 횟수
	LastError       error     `json:"last_error"`        // 마지막 발생 에러
	CreatedAt       time.Time `json:"created_at"`        // 구독 생성 시간
	LastReconnectAt time.Time `json:"last_reconnect_at"` // 마지막 재연결 시간
	EventCount      int64     `json:"event_count"`       // 총 수신 이벤트 수
}

// WatcherConfig는 Watcher 설정을 정의하는 구조체
type WatcherConfig struct {
	ChannelSize      int           `json:"channel_size"`       // 이벤트 채널 버퍼 크기
	ErrorChannelSize int           `json:"error_channel_size"` // 에러 채널 버퍼 크기
	MaxRetries       int           `json:"max_retries"`        // 최대 재시도 횟수
	RetryDelay       time.Duration `json:"retry_delay"`        // 재시도 지연 시간
	HealthCheckInt   time.Duration `json:"health_check_int"`   // 헬스체크 간격
}

// WatcherMetrics는 Watcher의 성능 메트릭을 추적하는 구조체
type WatcherMetrics struct {
	types.BaseMetrics
	TotalReconnects int64   `json:"total_reconnects"`   // 총 재연결 횟수
	ActiveSubs      int32   `json:"active_subs"`        // 활성 구독 수
	AvgEventsPerMin float64 `json:"avg_events_per_min"` // 분당 평균 이벤트 수
}
