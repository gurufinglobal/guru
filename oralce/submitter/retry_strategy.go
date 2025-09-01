package submitter

import (
	"math"
	"math/rand"
	"strings"
	"time"
)

// ExponentialBackoffRetry는 지수 백오프 재시도 전략을 구현
type ExponentialBackoffRetry struct {
	InitialDelay time.Duration // 초기 지연 시간
	MaxDelay     time.Duration // 최대 지연 시간
	Multiplier   float64       // 증가 배수
	MaxRetries   int           // 최대 재시도 횟수
	Jitter       bool          // 지터 사용 여부
}

// NewExponentialBackoffRetry는 지수 백오프 재시도 전략을 생성
func NewExponentialBackoffRetry(
	initialDelay, maxDelay time.Duration,
	multiplier float64,
	maxRetries int,
	jitter bool,
) *ExponentialBackoffRetry {
	return &ExponentialBackoffRetry{
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   multiplier,
		MaxRetries:   maxRetries,
		Jitter:       jitter,
	}
}

// ShouldRetry는 에러가 재시도 가능한지 확인
func (r *ExponentialBackoffRetry) ShouldRetry(err error, attempt int) bool {
	// 최대 재시도 횟수 초과 확인
	if attempt >= r.MaxRetries {
		return false
	}

	// 에러 타입별 재시도 가능성 확인
	return r.isRetryableError(err)
}

// GetDelay는 재시도 간격을 반환
func (r *ExponentialBackoffRetry) GetDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// 지수적 증가 계산
	delay := float64(r.InitialDelay) * math.Pow(r.Multiplier, float64(attempt))

	// 최대 지연 시간 제한
	if delay > float64(r.MaxDelay) {
		delay = float64(r.MaxDelay)
	}

	duration := time.Duration(delay)

	// 지터 적용
	if r.Jitter {
		jitterAmount := float64(duration) * 0.1 // 10% 지터
		jitterOffset := (rand.Float64() - 0.5) * 2 * jitterAmount
		duration += time.Duration(jitterOffset)

		// 음수가 되지 않도록 보장
		if duration < 0 {
			duration = r.InitialDelay
		}
	}

	return duration
}

// GetMaxRetries는 최대 재시도 횟수를 반환
func (r *ExponentialBackoffRetry) GetMaxRetries() int {
	return r.MaxRetries
}

// Reset은 재시도 전략을 초기 상태로 리셋 (상태가 없으므로 no-op)
func (r *ExponentialBackoffRetry) Reset() {
	// 상태가 없는 전략이므로 아무것도 하지 않음
}

// isRetryableError는 에러가 재시도 가능한지 확인
func (r *ExponentialBackoffRetry) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// 트랜잭션 에러 확인
	if txErr, ok := err.(*TransactionError); ok {
		return r.isRetryableTransactionError(txErr)
	}

	// 시퀀스 에러는 항상 재시도 가능
	if _, ok := err.(*SequenceError); ok {
		return true
	}

	// 에러 메시지 기반 판단
	errMsg := strings.ToLower(err.Error())

	// 재시도 가능한 에러들
	retryablePatterns := []string{
		"timeout",
		"connection",
		"network",
		"temporary",
		"too many requests",
		"rate limit",
		"sequence mismatch",
		"account sequence",
		"mempool is full",
		"tx already exists",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	// 재시도 불가능한 에러들
	nonRetryablePatterns := []string{
		"invalid",
		"unauthorized",
		"insufficient funds",
		"signature verification failed",
		"malformed",
		"out of gas",
	}

	for _, pattern := range nonRetryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return false
		}
	}

	// 알 수 없는 에러는 일단 재시도 가능으로 간주
	return true
}

// isRetryableTransactionError는 트랜잭션 에러가 재시도 가능한지 확인
func (r *ExponentialBackoffRetry) isRetryableTransactionError(txErr *TransactionError) bool {
	// 이미 재시도 가능성이 설정된 경우 그 값 사용
	if txErr.Retryable {
		return true
	}

	// Cosmos SDK 에러 코드별 판단
	switch txErr.Code {
	// 재시도 가능한 에러들
	case 32: // account sequence mismatch
		return true
	case 19: // tx already in mempool
		return true
	case 11: // out of gas (가스 부족은 가스 조정 후 재시도 가능)
		return true
	case 13: // insufficient fee
		return true

	// 재시도 불가능한 에러들
	case 2: // invalid sequence
		return false
	case 4: // unauthorized
		return false
	case 5: // insufficient funds
		return false
	case 7: // invalid address
		return false
	case 9: // unknown request
		return false

	// 기본적으로 재시도 가능
	default:
		return true
	}
}

// LinearBackoffRetry는 선형 백오프 재시도 전략을 구현
type LinearBackoffRetry struct {
	BaseDelay  time.Duration // 기본 지연 시간
	Increment  time.Duration // 증가량
	MaxDelay   time.Duration // 최대 지연 시간
	MaxRetries int           // 최대 재시도 횟수
	Jitter     bool          // 지터 사용 여부
}

// NewLinearBackoffRetry는 선형 백오프 재시도 전략을 생성
func NewLinearBackoffRetry(
	baseDelay, increment, maxDelay time.Duration,
	maxRetries int,
	jitter bool,
) *LinearBackoffRetry {
	return &LinearBackoffRetry{
		BaseDelay:  baseDelay,
		Increment:  increment,
		MaxDelay:   maxDelay,
		MaxRetries: maxRetries,
		Jitter:     jitter,
	}
}

// ShouldRetry는 에러가 재시도 가능한지 확인
func (r *LinearBackoffRetry) ShouldRetry(err error, attempt int) bool {
	if attempt >= r.MaxRetries {
		return false
	}

	// 기본 재시도 로직 사용
	strategy := &ExponentialBackoffRetry{}
	return strategy.isRetryableError(err)
}

// GetDelay는 재시도 간격을 반환
func (r *LinearBackoffRetry) GetDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// 선형 증가 계산
	delay := r.BaseDelay + time.Duration(attempt)*r.Increment

	// 최대 지연 시간 제한
	if delay > r.MaxDelay {
		delay = r.MaxDelay
	}

	// 지터 적용
	if r.Jitter && delay > 0 {
		jitterAmount := delay / 10 // 10% 지터
		jitterOffset := time.Duration((rand.Float64() - 0.5) * 2 * float64(jitterAmount))
		delay += jitterOffset

		// 음수가 되지 않도록 보장
		if delay < 0 {
			delay = r.BaseDelay
		}
	}

	return delay
}

// GetMaxRetries는 최대 재시도 횟수를 반환
func (r *LinearBackoffRetry) GetMaxRetries() int {
	return r.MaxRetries
}

// Reset은 재시도 전략을 초기 상태로 리셋
func (r *LinearBackoffRetry) Reset() {
	// 상태가 없는 전략이므로 아무것도 하지 않음
}

// FixedDelayRetry는 고정 지연 재시도 전략을 구현
type FixedDelayRetry struct {
	Delay      time.Duration // 고정 지연 시간
	MaxRetries int           // 최대 재시도 횟수
	Jitter     bool          // 지터 사용 여부
}

// NewFixedDelayRetry는 고정 지연 재시도 전략을 생성
func NewFixedDelayRetry(
	delay time.Duration,
	maxRetries int,
	jitter bool,
) *FixedDelayRetry {
	return &FixedDelayRetry{
		Delay:      delay,
		MaxRetries: maxRetries,
		Jitter:     jitter,
	}
}

// ShouldRetry는 에러가 재시도 가능한지 확인
func (r *FixedDelayRetry) ShouldRetry(err error, attempt int) bool {
	if attempt >= r.MaxRetries {
		return false
	}

	// 기본 재시도 로직 사용
	strategy := &ExponentialBackoffRetry{}
	return strategy.isRetryableError(err)
}

// GetDelay는 재시도 간격을 반환
func (r *FixedDelayRetry) GetDelay(attempt int) time.Duration {
	delay := r.Delay

	// 지터 적용
	if r.Jitter && delay > 0 {
		jitterAmount := delay / 10 // 10% 지터
		jitterOffset := time.Duration((rand.Float64() - 0.5) * 2 * float64(jitterAmount))
		delay += jitterOffset

		// 음수가 되지 않도록 보장
		if delay < 0 {
			delay = r.Delay
		}
	}

	return delay
}

// GetMaxRetries는 최대 재시도 횟수를 반환
func (r *FixedDelayRetry) GetMaxRetries() int {
	return r.MaxRetries
}

// Reset은 재시도 전략을 초기 상태로 리셋
func (r *FixedDelayRetry) Reset() {
	// 상태가 없는 전략이므로 아무것도 하지 않음
}

// AdaptiveRetry는 적응적 재시도 전략을 구현 (성공률에 따라 조정)
type AdaptiveRetry struct {
	baseStrategy RetryStrategy // 기본 전략
	successCount int           // 성공 횟수
	failureCount int           // 실패 횟수
	adjustment   float64       // 조정 비율
}

// NewAdaptiveRetry는 적응적 재시도 전략을 생성
func NewAdaptiveRetry(baseStrategy RetryStrategy) *AdaptiveRetry {
	return &AdaptiveRetry{
		baseStrategy: baseStrategy,
		adjustment:   1.0,
	}
}

// ShouldRetry는 에러가 재시도 가능한지 확인
func (r *AdaptiveRetry) ShouldRetry(err error, attempt int) bool {
	return r.baseStrategy.ShouldRetry(err, attempt)
}

// GetDelay는 성공률을 고려하여 조정된 지연 시간을 반환
func (r *AdaptiveRetry) GetDelay(attempt int) time.Duration {
	baseDelay := r.baseStrategy.GetDelay(attempt)

	// 성공률이 낮으면 지연 시간 증가, 높으면 감소
	totalAttempts := r.successCount + r.failureCount
	if totalAttempts > 10 { // 충분한 샘플이 있을 때만 조정
		successRate := float64(r.successCount) / float64(totalAttempts)

		if successRate < 0.3 { // 성공률 30% 미만
			r.adjustment = math.Min(r.adjustment*1.5, 3.0) // 최대 3배까지
		} else if successRate > 0.7 { // 성공률 70% 이상
			r.adjustment = math.Max(r.adjustment*0.7, 0.3) // 최소 0.3배까지
		}
	}

	adjustedDelay := time.Duration(float64(baseDelay) * r.adjustment)
	return adjustedDelay
}

// GetMaxRetries는 최대 재시도 횟수를 반환
func (r *AdaptiveRetry) GetMaxRetries() int {
	return r.baseStrategy.GetMaxRetries()
}

// Reset은 적응적 재시도 전략의 통계를 초기화
func (r *AdaptiveRetry) Reset() {
	r.successCount = 0
	r.failureCount = 0
	r.adjustment = 1.0
	if r.baseStrategy != nil {
		r.baseStrategy.Reset()
	}
}

// RecordSuccess는 성공 시도를 기록
func (r *AdaptiveRetry) RecordSuccess() {
	r.successCount++
}

// RecordFailure는 실패 시도를 기록
func (r *AdaptiveRetry) RecordFailure() {
	r.failureCount++
}

// GetStats는 현재 통계를 반환
func (r *AdaptiveRetry) GetStats() (successCount, failureCount int, successRate float64) {
	total := r.successCount + r.failureCount
	if total > 0 {
		successRate = float64(r.successCount) / float64(total)
	}
	return r.successCount, r.failureCount, successRate
}

// 기본 재시도 전략 팩토리 함수들

// NewDefaultRetryStrategy는 기본 재시도 전략을 생성
func NewDefaultRetryStrategy() RetryStrategy {
	return NewExponentialBackoffRetry(
		1*time.Second,  // 초기 1초
		30*time.Second, // 최대 30초
		2.0,            // 2배씩 증가
		5,              // 최대 5번 재시도
		true,           // 지터 사용
	)
}

// NewAggressiveRetryStrategy는 적극적인 재시도 전략을 생성
func NewAggressiveRetryStrategy() RetryStrategy {
	return NewExponentialBackoffRetry(
		500*time.Millisecond, // 초기 0.5초
		10*time.Second,       // 최대 10초
		1.5,                  // 1.5배씩 증가
		8,                    // 최대 8번 재시도
		true,                 // 지터 사용
	)
}

// NewConservativeRetryStrategy는 보수적인 재시도 전략을 생성
func NewConservativeRetryStrategy() RetryStrategy {
	return NewLinearBackoffRetry(
		2*time.Second,  // 기본 2초
		1*time.Second,  // 1초씩 증가
		15*time.Second, // 최대 15초
		3,              // 최대 3번 재시도
		false,          // 지터 비사용
	)
}

// RetryResult는 재시도 결과를 나타내는 구조체
type RetryResult struct {
	Success     bool          `json:"success"`
	Attempts    int           `json:"attempts"`
	TotalDelay  time.Duration `json:"total_delay"`
	LastError   error         `json:"last_error"`
	FinalResult interface{}   `json:"final_result"`
}

// RetryWithStrategy는 지정된 전략으로 함수를 재시도 실행
func RetryWithStrategy(
	strategy RetryStrategy,
	operation func() error,
) RetryResult {

	var lastErr error
	totalDelay := time.Duration(0)

	for attempt := 0; attempt <= strategy.GetMaxRetries(); attempt++ {
		if attempt > 0 {
			// 재시도 지연
			delay := strategy.GetDelay(attempt - 1)
			time.Sleep(delay)
			totalDelay += delay
		}

		err := operation()
		if err == nil {
			// 성공
			return RetryResult{
				Success:    true,
				Attempts:   attempt + 1,
				TotalDelay: totalDelay,
			}
		}

		lastErr = err

		// 재시도 가능성 확인
		if !strategy.ShouldRetry(err, attempt) {
			break
		}
	}

	// 모든 재시도 실패
	return RetryResult{
		Success:    false,
		Attempts:   strategy.GetMaxRetries() + 1,
		TotalDelay: totalDelay,
		LastError:  lastErr,
	}
}
