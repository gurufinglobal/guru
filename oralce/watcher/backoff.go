package watcher

import (
	"math"
	"math/rand"
	"time"
)

// ExponentialBackoff는 지수 백오프 재연결 전략을 구현
type ExponentialBackoff struct {
	InitialDelay time.Duration // 초기 지연 시간
	MaxDelay     time.Duration // 최대 지연 시간
	Multiplier   float64       // 증가 배수
	Jitter       bool          // 지터 사용 여부
	MaxAttempts  int           // 최대 시도 횟수
}

// NewExponentialBackoff는 새로운 지수 백오프 전략을 생성
func NewExponentialBackoff(initialDelay, maxDelay time.Duration, multiplier float64, jitter bool, maxAttempts int) *ExponentialBackoff {
	return &ExponentialBackoff{
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   multiplier,
		Jitter:       jitter,
		MaxAttempts:  maxAttempts,
	}
}

// Next는 현재 시도 횟수에 따른 다음 지연 시간을 계산
func (e *ExponentialBackoff) Next(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// 최대 시도 횟수 초과 시 최대 지연 시간 반환
	if e.MaxAttempts > 0 && attempt >= e.MaxAttempts {
		return e.MaxDelay
	}

	// 지수적으로 증가하는 지연 시간 계산
	delay := float64(e.InitialDelay) * math.Pow(e.Multiplier, float64(attempt))

	// 최대 지연 시간 제한
	if delay > float64(e.MaxDelay) {
		delay = float64(e.MaxDelay)
	}

	// 지터 적용 (무작위성 추가로 thundering herd 방지)
	if e.Jitter && delay > 0 {
		jitterAmount := delay * 0.1 // 10% 지터
		jitterOffset := (rand.Float64() - 0.5) * 2 * jitterAmount
		delay += jitterOffset

		// 음수가 되지 않도록 보장
		if delay < 0 {
			delay = float64(e.InitialDelay)
		}
	}

	return time.Duration(delay)
}

// Reset은 백오프 전략을 초기 상태로 리셋 (현재 구현에서는 상태가 없으므로 no-op)
func (e *ExponentialBackoff) Reset() {
	// 상태가 없는 백오프 전략이므로 아무것도 하지 않음
}

// LinearBackoff는 선형 증가 백오프 전략을 구현
type LinearBackoff struct {
	InitialDelay time.Duration // 초기 지연 시간
	MaxDelay     time.Duration // 최대 지연 시간
	Increment    time.Duration // 증가량
	Jitter       bool          // 지터 사용 여부
	MaxAttempts  int           // 최대 시도 횟수
}

// NewLinearBackoff는 새로운 선형 백오프 전략을 생성
func NewLinearBackoff(initialDelay, maxDelay, increment time.Duration, jitter bool, maxAttempts int) *LinearBackoff {
	return &LinearBackoff{
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Increment:    increment,
		Jitter:       jitter,
		MaxAttempts:  maxAttempts,
	}
}

// Next는 현재 시도 횟수에 따른 다음 지연 시간을 계산
func (l *LinearBackoff) Next(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// 최대 시도 횟수 초과 시 최대 지연 시간 반환
	if l.MaxAttempts > 0 && attempt >= l.MaxAttempts {
		return l.MaxDelay
	}

	// 선형적으로 증가하는 지연 시간 계산
	delay := l.InitialDelay + time.Duration(attempt)*l.Increment

	// 최대 지연 시간 제한
	if delay > l.MaxDelay {
		delay = l.MaxDelay
	}

	// 지터 적용
	if l.Jitter && delay > 0 {
		jitterAmount := delay / 10 // 10% 지터
		jitterOffset := time.Duration((rand.Float64() - 0.5) * 2 * float64(jitterAmount))
		delay += jitterOffset

		// 음수가 되지 않도록 보장
		if delay < 0 {
			delay = l.InitialDelay
		}
	}

	return delay
}

// Reset은 백오프 전략을 초기 상태로 리셋
func (l *LinearBackoff) Reset() {
	// 상태가 없는 백오프 전략이므로 아무것도 하지 않음
}

// FixedBackoff는 고정 지연 시간 백오프 전략을 구현
type FixedBackoff struct {
	Delay       time.Duration // 고정 지연 시간
	Jitter      bool          // 지터 사용 여부
	MaxAttempts int           // 최대 시도 횟수
}

// NewFixedBackoff는 새로운 고정 백오프 전략을 생성
func NewFixedBackoff(delay time.Duration, jitter bool, maxAttempts int) *FixedBackoff {
	return &FixedBackoff{
		Delay:       delay,
		Jitter:      jitter,
		MaxAttempts: maxAttempts,
	}
}

// Next는 현재 시도 횟수와 관계없이 고정된 지연 시간을 반환
func (f *FixedBackoff) Next(attempt int) time.Duration {
	// 최대 시도 횟수 초과 시 고정 지연 시간 반환 (무한 재시도 방지)
	if f.MaxAttempts > 0 && attempt >= f.MaxAttempts {
		return f.Delay
	}

	delay := f.Delay

	// 지터 적용
	if f.Jitter && delay > 0 {
		jitterAmount := delay / 10 // 10% 지터
		jitterOffset := time.Duration((rand.Float64() - 0.5) * 2 * float64(jitterAmount))
		delay += jitterOffset

		// 음수가 되지 않도록 보장
		if delay < 0 {
			delay = f.Delay
		}
	}

	return delay
}

// Reset은 백오프 전략을 초기 상태로 리셋
func (f *FixedBackoff) Reset() {
	// 상태가 없는 백오프 전략이므로 아무것도 하지 않음
}

// AdaptiveBackoff는 적응적 백오프 전략을 구현 (성공률에 따라 조정)
type AdaptiveBackoff struct {
	baseStrategy BackoffStrategy // 기본 전략
	successCount int             // 성공 횟수
	failureCount int             // 실패 횟수
	adjustment   float64         // 조정 비율
}

// NewAdaptiveBackoff는 새로운 적응적 백오프 전략을 생성
func NewAdaptiveBackoff(baseStrategy BackoffStrategy) *AdaptiveBackoff {
	return &AdaptiveBackoff{
		baseStrategy: baseStrategy,
		adjustment:   1.0,
	}
}

// Next는 성공률을 고려하여 조정된 지연 시간을 반환
func (a *AdaptiveBackoff) Next(attempt int) time.Duration {
	baseDelay := a.baseStrategy.Next(attempt)

	// 성공률이 낮으면 지연 시간을 증가, 높으면 감소
	totalAttempts := a.successCount + a.failureCount
	if totalAttempts > 0 {
		successRate := float64(a.successCount) / float64(totalAttempts)

		// 성공률이 50% 미만이면 지연 시간 증가, 70% 이상이면 감소
		if successRate < 0.5 {
			a.adjustment = math.Min(a.adjustment*1.2, 3.0) // 최대 3배까지
		} else if successRate > 0.7 {
			a.adjustment = math.Max(a.adjustment*0.8, 0.5) // 최소 0.5배까지
		}
	}

	adjustedDelay := time.Duration(float64(baseDelay) * a.adjustment)
	return adjustedDelay
}

// Reset은 적응적 백오프 전략의 통계를 초기화
func (a *AdaptiveBackoff) Reset() {
	a.successCount = 0
	a.failureCount = 0
	a.adjustment = 1.0
	if a.baseStrategy != nil {
		a.baseStrategy.Reset()
	}
}

// RecordSuccess는 성공 시도를 기록
func (a *AdaptiveBackoff) RecordSuccess() {
	a.successCount++
}

// RecordFailure는 실패 시도를 기록
func (a *AdaptiveBackoff) RecordFailure() {
	a.failureCount++
}

// GetStats는 현재 통계를 반환
func (a *AdaptiveBackoff) GetStats() (successCount, failureCount int, successRate float64) {
	total := a.successCount + a.failureCount
	if total > 0 {
		successRate = float64(a.successCount) / float64(total)
	}
	return a.successCount, a.failureCount, successRate
}

// 기본 백오프 전략 팩토리 함수들

// NewDefaultExponentialBackoff는 기본 지수 백오프 전략을 생성
func NewDefaultExponentialBackoff() BackoffStrategy {
	return NewExponentialBackoff(
		1*time.Second,  // 초기 1초
		30*time.Second, // 최대 30초
		2.0,            // 2배씩 증가
		true,           // 지터 사용
		10,             // 최대 10번 시도
	)
}

// NewDefaultLinearBackoff는 기본 선형 백오프 전략을 생성
func NewDefaultLinearBackoff() BackoffStrategy {
	return NewLinearBackoff(
		2*time.Second,  // 초기 2초
		20*time.Second, // 최대 20초
		2*time.Second,  // 2초씩 증가
		true,           // 지터 사용
		10,             // 최대 10번 시도
	)
}

// NewDefaultFixedBackoff는 기본 고정 백오프 전략을 생성
func NewDefaultFixedBackoff() BackoffStrategy {
	return NewFixedBackoff(
		5*time.Second, // 고정 5초
		true,          // 지터 사용
		0,             // 무제한 시도
	)
}
