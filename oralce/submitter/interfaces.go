package submitter

import (
	"context"
	"fmt"
	"time"

	"github.com/GPTx-global/guru-v2/oralce/scheduler"
	commontypes "github.com/GPTx-global/guru-v2/oralce/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// JobResultSubmitter는 Oracle 작업 결과를 블록체인에 제출하는 메인 인터페이스
type JobResultSubmitter interface {
	// Start는 서브미터를 시작
	Start(ctx context.Context) error

	// Stop은 서브미터를 중지하고 리소스를 정리
	Stop() error

	// Submit은 단일 작업 결과를 제출
	Submit(ctx context.Context, result *scheduler.JobResult) error

	// SubmitBatch는 여러 작업 결과를 일괄 제출
	SubmitBatch(ctx context.Context, results []*scheduler.JobResult) error

	// GetMetrics는 현재 제출 메트릭을 반환
	GetMetrics() SubmitterMetrics

	// IsRunning은 서브미터 실행 상태를 반환
	IsRunning() bool
}

// TransactionBuilder는 트랜잭션 생성을 담당하는 인터페이스
type TransactionBuilder interface {
	// BuildSubmitTx는 Oracle 데이터 제출 트랜잭션을 생성
	BuildSubmitTx(ctx context.Context, result *scheduler.JobResult) ([]byte, error)

	// EstimateGas는 트랜잭션의 가스를 추정
	EstimateGas(ctx context.Context, result *scheduler.JobResult) (uint64, error)

	// UpdateGasPrice는 가스 가격을 업데이트
	UpdateGasPrice(gasPrice string) error

	// GetCurrentSequence는 현재 시퀀스 번호를 반환
	GetCurrentSequence() uint64

	// IncrementSequence는 시퀀스 번호를 증가
	IncrementSequence()

	// ResetSequence는 시퀀스 번호를 초기화
	ResetSequence(ctx context.Context) error
}

// SequenceManager는 계정 시퀀스 번호를 관리하는 인터페이스
type SequenceManager interface {
	// GetSequence는 현재 시퀀스 번호를 반환
	GetSequence() uint64

	// NextSequence는 다음 시퀀스 번호를 반환하고 내부 카운터를 증가
	NextSequence() uint64

	// UpdateSequence는 시퀀스를 특정 값으로 업데이트
	UpdateSequence(sequence uint64)

	// SyncWithChain은 체인의 실제 시퀀스와 동기화
	SyncWithChain(ctx context.Context) error

	// HandleSequenceError는 시퀀스 관련 에러를 처리하고 복구를 시도
	HandleSequenceError(ctx context.Context, err error) error
}

// RetryStrategy는 commontypes.RetryStrategy의 별칭
// Deprecated: commontypes.RetryStrategy를 직접 사용하세요
type RetryStrategy = commontypes.RetryStrategy

// BroadcastClient는 트랜잭션 브로드캐스팅을 담당하는 인터페이스
type BroadcastClient interface {
	// BroadcastTx는 트랜잭션을 브로드캐스트
	BroadcastTx(ctx context.Context, txBytes []byte) (*sdk.TxResponse, error)

	// BroadcastTxSync는 동기식으로 트랜잭션을 브로드캐스트
	BroadcastTxSync(ctx context.Context, txBytes []byte) (*sdk.TxResponse, error)

	// BroadcastTxAsync는 비동기식으로 트랜잭션을 브로드캐스트
	BroadcastTxAsync(ctx context.Context, txBytes []byte) (*sdk.TxResponse, error)

	// SimulateTx는 트랜잭션을 시뮬레이션
	SimulateTx(ctx context.Context, txBytes []byte) (*sdk.SimulationResponse, error)
}

// 데이터 구조체들

// SubmitRequest는 제출 요청을 나타내는 구조체
type SubmitRequest struct {
	JobResult  *scheduler.JobResult `json:"job_result"`
	Priority   int                  `json:"priority"`
	MaxRetries int                  `json:"max_retries"`
	Timeout    time.Duration        `json:"timeout"`
	CreatedAt  time.Time            `json:"created_at"`
}

// SubmitResult는 제출 결과를 나타내는 구조체
type SubmitResult struct {
	JobID       string        `json:"job_id"`
	TxHash      string        `json:"tx_hash"`
	Success     bool          `json:"success"`
	Error       error         `json:"error,omitempty"`
	Attempts    int           `json:"attempts"`
	Duration    time.Duration `json:"duration"`
	GasUsed     uint64        `json:"gas_used"`
	GasFee      sdk.Coins     `json:"gas_fee"`
	SubmittedAt time.Time     `json:"submitted_at"`
}

// SubmitterMetrics는 서브미터 메트릭을 나타내는 구조체
type SubmitterMetrics struct {
	commontypes.BaseMetrics

	// 제출 통계
	TotalSubmissions  int64 `json:"total_submissions"`
	SuccessfulSubmits int64 `json:"successful_submits"`
	FailedSubmits     int64 `json:"failed_submits"`
	PendingSubmits    int64 `json:"pending_submits"`

	// 성능 통계
	AvgSubmissionTime time.Duration `json:"avg_submission_time"`
	AvgGasUsed        uint64        `json:"avg_gas_used"`
	TotalGasFees      sdk.Coins     `json:"total_gas_fees"`
	SuccessRate       float64       `json:"success_rate"`

	// 에러 통계
	SequenceErrors      int64 `json:"sequence_errors"`
	NetworkErrors       int64 `json:"network_errors"`
	InsufficientFeeErrs int64 `json:"insufficient_fee_errors"`
	OtherErrors         int64 `json:"other_errors"`

	// 재시도 통계
	TotalRetries    int64 `json:"total_retries"`
	MaxRetryReached int64 `json:"max_retry_reached"`

	// 시간 정보
	LastSubmissionAt time.Time `json:"last_submission_at"`
}

// SubmitterConfig는 서브미터 설정을 나타내는 구조체
type SubmitterConfig struct {
	MaxConcurrentSubmits  int           `json:"max_concurrent_submits"`
	SubmissionTimeout     time.Duration `json:"submission_timeout"`
	DefaultMaxRetries     int           `json:"default_max_retries"`
	RetryDelay            time.Duration `json:"retry_delay"`
	SequenceSyncInterval  time.Duration `json:"sequence_sync_interval"`
	MetricsUpdateInterval time.Duration `json:"metrics_update_interval"`
	BatchSize             int           `json:"batch_size"`
	GasAdjustment         float64       `json:"gas_adjustment"`
}

// TransactionError는 트랜잭션 에러를 나타내는 구조체
type TransactionError struct {
	JobID     string `json:"job_id"`
	TxHash    string `json:"tx_hash,omitempty"`
	Code      uint32 `json:"code"`
	Codespace string `json:"codespace,omitempty"`
	Message   string `json:"message"`
	RawLog    string `json:"raw_log,omitempty"`
	GasUsed   uint64 `json:"gas_used"`
	GasWanted uint64 `json:"gas_wanted"`
	Original  error  `json:"-"`
	Retryable bool   `json:"retryable"`
}

// Error는 error 인터페이스 구현
func (e *TransactionError) Error() string {
	if e.TxHash != "" {
		return "transaction error for job '" + e.JobID + "' (tx: " + e.TxHash + "): " + e.Message
	}
	return "transaction error for job '" + e.JobID + "': " + e.Message
}

// Unwrap은 원본 에러를 반환
func (e *TransactionError) Unwrap() error {
	return e.Original
}

// IsRetryable은 에러가 재시도 가능한지 확인
func (e *TransactionError) IsRetryable() bool {
	return e.Retryable
}

// SequenceError는 시퀀스 에러를 나타내는 구조체
type SequenceError struct {
	ExpectedSeq uint64 `json:"expected_sequence"`
	ActualSeq   uint64 `json:"actual_sequence"`
	Message     string `json:"message"`
	Original    error  `json:"-"`
}

// Error는 error 인터페이스 구현
func (e *SequenceError) Error() string {
	return fmt.Sprintf("sequence error: expected %d, got %d: %s", e.ExpectedSeq, e.ActualSeq, e.Message)
}

// Unwrap은 원본 에러를 반환
func (e *SequenceError) Unwrap() error {
	return e.Original
}

// ClientContext는 Cosmos SDK 클라이언트 컨텍스트를 래핑하는 인터페이스
type ClientContext interface {
	// GetFromAddress는 발신자 주소를 반환
	GetFromAddress() sdk.AccAddress

	// GetFromName는 키 이름을 반환
	GetFromName() string

	// GetChainID는 체인 ID를 반환
	GetChainID() string

	// BroadcastTx는 트랜잭션을 브로드캐스트
	BroadcastTx(txBytes []byte) (*sdk.TxResponse, error)

	// Simulate는 트랜잭션을 시뮬레이션
	Simulate(txBytes []byte) (*sdk.SimulationResponse, error)

	// GetTxConfig는 트랜잭션 설정을 반환
	GetTxConfig() client.TxConfig

	// GetAccountRetriever는 계정 조회기를 반환
	GetAccountRetriever() client.AccountRetriever

	// GetKeyring은 키링을 반환
	GetKeyring() keyring.Keyring
}

// DefaultClientContext는 기본 클라이언트 컨텍스트 어댑터
type DefaultClientContext struct {
	clientCtx client.Context
}

// NewDefaultClientContext는 새로운 기본 클라이언트 컨텍스트를 생성
func NewDefaultClientContext(clientCtx client.Context) ClientContext {
	return &DefaultClientContext{clientCtx: clientCtx}
}

// GetFromAddress는 발신자 주소를 반환
func (c *DefaultClientContext) GetFromAddress() sdk.AccAddress {
	return c.clientCtx.GetFromAddress()
}

// GetFromName는 키 이름을 반환
func (c *DefaultClientContext) GetFromName() string {
	return c.clientCtx.GetFromName()
}

// GetChainID는 체인 ID를 반환
func (c *DefaultClientContext) GetChainID() string {
	return c.clientCtx.ChainID
}

// BroadcastTx는 트랜잭션을 브로드캐스트
func (c *DefaultClientContext) BroadcastTx(txBytes []byte) (*sdk.TxResponse, error) {
	return c.clientCtx.BroadcastTx(txBytes)
}

// Simulate는 트랜잭션을 시뮬레이션
func (c *DefaultClientContext) Simulate(txBytes []byte) (*sdk.SimulationResponse, error) {
	// client.Context.Simulate는 메서드가 아니라 불린 필드일 수 있으므로,
	// 실제로는 client.Context의 다른 메서드를 사용해야 할 수도 있습니다
	// 임시로 에러를 반환합니다
	return nil, fmt.Errorf("simulate not implemented")
}

// GetTxConfig는 트랜잭션 설정을 반환
func (c *DefaultClientContext) GetTxConfig() client.TxConfig {
	return c.clientCtx.TxConfig
}

// GetAccountRetriever는 계정 조회기를 반환
func (c *DefaultClientContext) GetAccountRetriever() client.AccountRetriever {
	return c.clientCtx.AccountRetriever
}

// GetKeyring은 키링을 반환
func (c *DefaultClientContext) GetKeyring() keyring.Keyring {
	return c.clientCtx.Keyring
}

// TxFactory는 트랜잭션 팩토리를 래핑하는 인터페이스
type TxFactory interface {
	// BuildUnsignedTx는 서명되지 않은 트랜잭션을 생성
	BuildUnsignedTx(msgs ...sdk.Msg) (client.TxBuilder, error)

	// Sign은 트랜잭션에 서명
	Sign(ctx context.Context, txBuilder client.TxBuilder, overwrite bool) error

	// GetSequence는 시퀀스 번호를 반환
	GetSequence() uint64

	// WithSequence는 시퀀스 번호를 설정
	WithSequence(sequence uint64) TxFactory

	// WithGas는 가스 한도를 설정
	WithGas(gas uint64) TxFactory

	// WithGasPrice는 가스 가격을 설정
	WithGasPrice(gasPrice sdk.DecCoins) TxFactory
}

// DefaultTxFactory는 기본 트랜잭션 팩토리 어댑터
type DefaultTxFactory struct {
	factory tx.Factory
	keyName string
}

// NewDefaultTxFactory는 새로운 기본 트랜잭션 팩토리를 생성
func NewDefaultTxFactory(factory tx.Factory, keyName string) TxFactory {
	return &DefaultTxFactory{
		factory: factory,
		keyName: keyName,
	}
}

// BuildUnsignedTx는 서명되지 않은 트랜잭션을 생성
func (f *DefaultTxFactory) BuildUnsignedTx(msgs ...sdk.Msg) (client.TxBuilder, error) {
	return f.factory.BuildUnsignedTx(msgs...)
}

// Sign은 트랜잭션에 서명
func (f *DefaultTxFactory) Sign(ctx context.Context, txBuilder client.TxBuilder, overwrite bool) error {
	return tx.Sign(ctx, f.factory, f.keyName, txBuilder, overwrite)
}

// GetSequence는 시퀀스 번호를 반환
func (f *DefaultTxFactory) GetSequence() uint64 {
	return f.factory.Sequence()
}

// WithSequence는 시퀀스 번호를 설정
func (f *DefaultTxFactory) WithSequence(sequence uint64) TxFactory {
	return &DefaultTxFactory{
		factory: f.factory.WithSequence(sequence),
		keyName: f.keyName,
	}
}

// WithGas는 가스 한도를 설정
func (f *DefaultTxFactory) WithGas(gas uint64) TxFactory {
	return &DefaultTxFactory{
		factory: f.factory.WithGas(gas),
		keyName: f.keyName,
	}
}

// WithGasPrice는 가스 가격을 설정
func (f *DefaultTxFactory) WithGasPrice(gasPrice sdk.DecCoins) TxFactory {
	return &DefaultTxFactory{
		factory: f.factory.WithGasPrices(gasPrice.String()),
		keyName: f.keyName,
	}
}
