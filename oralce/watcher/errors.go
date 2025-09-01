package watcher

import (
	"errors"
	"fmt"
)

// 기본 에러 타입들
var (
	// ErrWatcherNotRunning은 Watcher가 실행되지 않은 상태에서 작업을 시도할 때 발생
	ErrWatcherNotRunning = errors.New("watcher is not running")

	// ErrWatcherAlreadyRunning은 이미 실행 중인 Watcher를 다시 시작하려 할 때 발생
	ErrWatcherAlreadyRunning = errors.New("watcher is already running")

	// ErrNoQueries는 구독할 쿼리가 제공되지 않았을 때 발생
	ErrNoQueries = errors.New("no queries provided for subscription")

	// ErrSubscriptionNotFound는 존재하지 않는 구독에 접근하려 할 때 발생
	ErrSubscriptionNotFound = errors.New("subscription not found")

	// ErrMaxRetriesExceeded는 최대 재시도 횟수를 초과했을 때 발생
	ErrMaxRetriesExceeded = errors.New("maximum retry attempts exceeded")

	// ErrInvalidEvent는 유효하지 않은 이벤트가 수신되었을 때 발생
	ErrInvalidEvent = errors.New("invalid event received")

	// ErrClientNotConnected는 클라이언트가 연결되지 않았을 때 발생
	ErrClientNotConnected = errors.New("subscription client is not connected")

	// ErrContextCancelled는 컨텍스트가 취소되었을 때 발생
	ErrContextCancelled = errors.New("context was cancelled")

	// ErrChannelClosed는 채널이 닫혔을 때 발생
	ErrChannelClosed = errors.New("channel was closed")
)

// SubscriptionError는 구독 관련 에러를 나타내는 구조체
type SubscriptionError struct {
	Query    string // 에러가 발생한 쿼리
	Message  string // 에러 메시지
	Original error  // 원본 에러
	Retry    bool   // 재시도 가능 여부
}

// Error는 error 인터페이스 구현
func (e SubscriptionError) Error() string {
	if e.Original != nil {
		return fmt.Sprintf("subscription error for query '%s': %s (original: %v)",
			e.Query, e.Message, e.Original)
	}
	return fmt.Sprintf("subscription error for query '%s': %s", e.Query, e.Message)
}

// Unwrap은 원본 에러를 반환 (Go 1.13+ error wrapping)
func (e SubscriptionError) Unwrap() error {
	return e.Original
}

// IsRetryable은 에러가 재시도 가능한지 확인
func (e SubscriptionError) IsRetryable() bool {
	return e.Retry
}

// NewSubscriptionError는 새로운 구독 에러를 생성
func NewSubscriptionError(query, message string, original error, retry bool) SubscriptionError {
	return SubscriptionError{
		Query:    query,
		Message:  message,
		Original: original,
		Retry:    retry,
	}
}

// ValidationError는 이벤트 검증 에러를 나타내는 구조체
type ValidationError struct {
	Field   string // 검증 실패한 필드
	Value   string // 검증 실패한 값
	Message string // 에러 메시지
}

// Error는 error 인터페이스 구현
func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s' with value '%s': %s",
		e.Field, e.Value, e.Message)
}

// NewValidationError는 새로운 검증 에러를 생성
func NewValidationError(field, value, message string) ValidationError {
	return ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// ReconnectError는 재연결 시도 중 발생한 에러를 나타내는 구조체
type ReconnectError struct {
	Query      string // 재연결 시도한 쿼리
	Attempt    int    // 시도 횟수
	MaxRetries int    // 최대 재시도 횟수
	LastError  error  // 마지막 발생한 에러
}

// Error는 error 인터페이스 구현
func (e ReconnectError) Error() string {
	return fmt.Sprintf("reconnection failed for query '%s' after %d/%d attempts: %v",
		e.Query, e.Attempt, e.MaxRetries, e.LastError)
}

// Unwrap은 마지막 에러를 반환
func (e ReconnectError) Unwrap() error {
	return e.LastError
}

// IsMaxRetriesReached는 최대 재시도 횟수에 도달했는지 확인
func (e ReconnectError) IsMaxRetriesReached() bool {
	return e.Attempt >= e.MaxRetries
}

// NewReconnectError는 새로운 재연결 에러를 생성
func NewReconnectError(query string, attempt, maxRetries int, lastError error) ReconnectError {
	return ReconnectError{
		Query:      query,
		Attempt:    attempt,
		MaxRetries: maxRetries,
		LastError:  lastError,
	}
}

// EventProcessingError는 이벤트 처리 중 발생한 에러를 나타내는 구조체
type EventProcessingError struct {
	Query     string // 이벤트가 발생한 쿼리
	EventType string // 이벤트 타입
	Message   string // 에러 메시지
	Original  error  // 원본 에러
	Fatal     bool   // 치명적 에러 여부
}

// Error는 error 인터페이스 구현
func (e EventProcessingError) Error() string {
	fatalStr := ""
	if e.Fatal {
		fatalStr = " [FATAL]"
	}

	if e.Original != nil {
		return fmt.Sprintf("event processing error%s for query '%s' (type: %s): %s (original: %v)",
			fatalStr, e.Query, e.EventType, e.Message, e.Original)
	}
	return fmt.Sprintf("event processing error%s for query '%s' (type: %s): %s",
		fatalStr, e.Query, e.EventType, e.Message)
}

// Unwrap은 원본 에러를 반환
func (e EventProcessingError) Unwrap() error {
	return e.Original
}

// IsFatal은 치명적 에러인지 확인
func (e EventProcessingError) IsFatal() bool {
	return e.Fatal
}

// NewEventProcessingError는 새로운 이벤트 처리 에러를 생성
func NewEventProcessingError(query, eventType, message string, original error, fatal bool) EventProcessingError {
	return EventProcessingError{
		Query:     query,
		EventType: eventType,
		Message:   message,
		Original:  original,
		Fatal:     fatal,
	}
}

// 에러 타입 확인을 위한 헬퍼 함수들

// IsRetryableError는 에러가 재시도 가능한지 확인
func IsRetryableError(err error) bool {
	var subErr SubscriptionError
	if errors.As(err, &subErr) {
		return subErr.IsRetryable()
	}

	var reconErr ReconnectError
	if errors.As(err, &reconErr) {
		return !reconErr.IsMaxRetriesReached()
	}

	return false
}

// IsFatalError는 에러가 치명적인지 확인
func IsFatalError(err error) bool {
	var procErr EventProcessingError
	if errors.As(err, &procErr) {
		return procErr.IsFatal()
	}

	// 특정 에러들은 항상 치명적으로 간주
	switch {
	case errors.Is(err, ErrMaxRetriesExceeded):
		return true
	case errors.Is(err, ErrClientNotConnected):
		return true
	default:
		return false
	}
}

// IsTemporaryError는 일시적인 에러인지 확인
func IsTemporaryError(err error) bool {
	// 네트워크 관련 일시적 에러들 확인
	switch {
	case errors.Is(err, ErrChannelClosed):
		return true
	case errors.Is(err, ErrContextCancelled):
		return false // Context 취소는 일시적이지 않음
	default:
		return IsRetryableError(err)
	}
}

// WrapError는 에러를 적절한 타입으로 래핑
func WrapError(err error, query, context string) error {
	if err == nil {
		return nil
	}

	// 이미 래핑된 에러인 경우 그대로 반환
	switch err.(type) {
	case SubscriptionError, ValidationError, ReconnectError, EventProcessingError:
		return err
	}

	// 컨텍스트에 따라 적절한 타입으로 래핑
	switch context {
	case "subscription":
		return NewSubscriptionError(query, "subscription failed", err, IsTemporaryError(err))
	case "validation":
		return NewValidationError("event", "", err.Error())
	case "reconnection":
		return NewReconnectError(query, 1, 3, err)
	case "processing":
		return NewEventProcessingError(query, "unknown", err.Error(), err, IsFatalError(err))
	default:
		return fmt.Errorf("watcher error in %s for query '%s': %w", context, query, err)
	}
}
