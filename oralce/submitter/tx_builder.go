package submitter

import (
	"context"
	"fmt"

	"cosmossdk.io/log"
	guruconfig "github.com/GPTx-global/guru-v2/cmd/gurud/config"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/scheduler"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

// TransactionBuilder_impl는 트랜잭션 빌더의 구현체
type TransactionBuilder_impl struct {
	logger          log.Logger
	config          *config.Config
	clientCtx       ClientContext
	sequenceManager SequenceManager

	// 트랜잭션 설정
	accountNumber uint64
	gasAdjustment float64
	gasLimit      uint64
	gasPrices     sdk.DecCoins
}

// NewTransactionBuilder는 새로운 트랜잭션 빌더를 생성
func NewTransactionBuilder(
	logger log.Logger,
	cfg *config.Config,
	clientCtx ClientContext,
	sequenceManager SequenceManager,
) (TransactionBuilder, error) {

	// 계정 번호 조회
	accountRetriever := clientCtx.GetAccountRetriever()
	if accountRetriever == nil {
		return nil, fmt.Errorf("account retriever is nil")
	}

	// ClientContext를 client.Context로 변환
	var actualClientCtx client.Context
	if adapter, ok := clientCtx.(*DefaultClientContext); ok {
		actualClientCtx = adapter.clientCtx
	} else {
		return nil, fmt.Errorf("invalid client context type")
	}

	accountNumber, _, err := accountRetriever.GetAccountNumberSequence(
		actualClientCtx,
		clientCtx.GetFromAddress(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get account number: %w", err)
	}

	// 가스 가격 파싱
	gasPrices, err := sdk.ParseDecCoins(cfg.GasPrices() + guruconfig.BaseDenom)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gas prices: %w", err)
	}

	return &TransactionBuilder_impl{
		logger:          logger,
		config:          cfg,
		clientCtx:       clientCtx,
		sequenceManager: sequenceManager,
		accountNumber:   accountNumber,
		gasAdjustment:   cfg.GasAdjustment(),
		gasLimit:        cfg.GasLimit(),
		gasPrices:       gasPrices,
	}, nil
}

// BuildSubmitTx는 Oracle 데이터 제출 트랜잭션을 생성
func (tb *TransactionBuilder_impl) BuildSubmitTx(ctx context.Context, result *scheduler.JobResult) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("job result cannot be nil")
	}

	if !result.Success {
		return nil, fmt.Errorf("cannot build transaction for failed job result")
	}

	// Oracle 데이터 제출 메시지 생성
	msg := &oracletypes.MsgSubmitOracleData{
		AuthorityAddress: tb.clientCtx.GetFromAddress().String(),
		DataSet: &oracletypes.SubmitDataSet{
			RequestId: result.RequestID,
			RawData:   result.Data,
			Nonce:     result.Nonce,
			Provider:  tb.clientCtx.GetFromAddress().String(),
			Signature: "", // Signature는 현재 Oracle 모듈에서 검증하지 않음
		},
	}

	// 트랜잭션 팩토리 생성
	txFactory := tb.createTxFactory()

	// 서명되지 않은 트랜잭션 빌드
	txBuilder, err := txFactory.BuildUnsignedTx(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to build unsigned transaction: %w", err)
	}

	// 가스 추정 (선택사항)
	if tb.gasLimit == 0 {
		estimatedGas, err := tb.EstimateGas(ctx, result)
		if err != nil {
			tb.logger.Warn("failed to estimate gas, using default", "error", err)
		} else {
			txBuilder.SetGasLimit(estimatedGas)
		}
	}

	// 트랜잭션 서명
	err = tx.Sign(ctx, txFactory, tb.config.KeyName(), txBuilder, true)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// 트랜잭션 인코딩
	txBytes, err := tb.clientCtx.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %w", err)
	}

	tb.logger.Debug("transaction built successfully",
		"job_id", result.JobID,
		"request_id", result.RequestID,
		"nonce", result.Nonce,
		"gas_limit", txBuilder.GetTx().GetGas(),
		"sequence", txFactory.Sequence())

	return txBytes, nil
}

// EstimateGas는 트랜잭션의 가스를 추정
func (tb *TransactionBuilder_impl) EstimateGas(ctx context.Context, result *scheduler.JobResult) (uint64, error) {
	if result == nil {
		return 0, fmt.Errorf("job result cannot be nil")
	}

	// 임시 트랜잭션 생성 (시뮬레이션용)
	msg := &oracletypes.MsgSubmitOracleData{
		AuthorityAddress: tb.clientCtx.GetFromAddress().String(),
		DataSet: &oracletypes.SubmitDataSet{
			RequestId: result.RequestID,
			RawData:   result.Data,
			Nonce:     result.Nonce,
			Provider:  tb.clientCtx.GetFromAddress().String(),
			Signature: "", // Signature는 현재 Oracle 모듈에서 검증하지 않음
		},
	}

	// 시뮬레이션을 위한 팩토리 생성 (가스 0으로 설정)
	simFactory := tb.createTxFactory().WithGas(0)

	txBuilder, err := simFactory.BuildUnsignedTx(msg)
	if err != nil {
		return 0, fmt.Errorf("failed to build simulation transaction: %w", err)
	}

	// 임시 서명 (시뮬레이션을 위해 필요)
	err = tx.Sign(ctx, simFactory, tb.config.KeyName(), txBuilder, true)
	if err != nil {
		return 0, fmt.Errorf("failed to sign simulation transaction: %w", err)
	}

	// 트랜잭션 시뮬레이션
	txBytes, err := tb.clientCtx.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return 0, fmt.Errorf("failed to encode simulation transaction: %w", err)
	}

	simRes, err := tb.clientCtx.Simulate(txBytes)
	if err != nil {
		return 0, fmt.Errorf("simulation failed: %w", err)
	}

	// 가스 조정 적용
	estimatedGas := uint64(float64(simRes.GasInfo.GasUsed) * tb.gasAdjustment)

	tb.logger.Debug("gas estimated",
		"job_id", result.JobID,
		"gas_used", simRes.GasInfo.GasUsed,
		"gas_wanted", simRes.GasInfo.GasWanted,
		"estimated_gas", estimatedGas,
		"adjustment", tb.gasAdjustment)

	return estimatedGas, nil
}

// UpdateGasPrice는 가스 가격을 업데이트
func (tb *TransactionBuilder_impl) UpdateGasPrice(gasPrice string) error {
	gasPrices, err := sdk.ParseDecCoins(gasPrice + guruconfig.BaseDenom)
	if err != nil {
		return fmt.Errorf("failed to parse gas price: %w", err)
	}

	tb.gasPrices = gasPrices

	tb.logger.Debug("gas price updated", "gas_prices", gasPrices.String())
	return nil
}

// GetCurrentSequence는 현재 시퀀스 번호를 반환
func (tb *TransactionBuilder_impl) GetCurrentSequence() uint64 {
	return tb.sequenceManager.GetSequence()
}

// IncrementSequence는 시퀀스 번호를 증가
func (tb *TransactionBuilder_impl) IncrementSequence() {
	tb.sequenceManager.NextSequence()
}

// ResetSequence는 시퀀스 번호를 초기화
func (tb *TransactionBuilder_impl) ResetSequence(ctx context.Context) error {
	return tb.sequenceManager.SyncWithChain(ctx)
}

// createTxFactory는 트랜잭션 팩토리를 생성
func (tb *TransactionBuilder_impl) createTxFactory() tx.Factory {
	sequence := tb.sequenceManager.GetSequence()

	return tx.Factory{}.
		WithTxConfig(tb.clientCtx.GetTxConfig()).
		WithAccountRetriever(tb.clientCtx.GetAccountRetriever()).
		WithKeybase(tb.clientCtx.GetKeyring()).
		WithChainID(tb.clientCtx.GetChainID()).
		WithGas(tb.gasLimit).
		WithGasAdjustment(tb.gasAdjustment).
		WithGasPrices(tb.gasPrices.String()).
		WithAccountNumber(tb.accountNumber).
		WithSequence(sequence).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT).
		WithSimulateAndExecute(false)
}

// BuildBatchSubmitTx는 여러 Oracle 데이터를 일괄 제출하는 트랜잭션을 생성
func (tb *TransactionBuilder_impl) BuildBatchSubmitTx(ctx context.Context, results []*scheduler.JobResult) ([]byte, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no results to submit")
	}

	var msgs []sdk.Msg

	// 각 결과를 메시지로 변환
	for _, result := range results {
		if result == nil || !result.Success {
			continue // 실패한 결과는 건너뛰기
		}

		msg := &oracletypes.MsgSubmitOracleData{
			AuthorityAddress: tb.clientCtx.GetFromAddress().String(),
			DataSet: &oracletypes.SubmitDataSet{
				RequestId: result.RequestID,
				RawData:   result.Data,
				Nonce:     result.Nonce,
				Provider:  tb.clientCtx.GetFromAddress().String(),
				Signature: "", // Signature는 현재 Oracle 모듈에서 검증하지 않음
			},
		}

		msgs = append(msgs, msg)
	}

	if len(msgs) == 0 {
		return nil, fmt.Errorf("no valid results to submit")
	}

	// 트랜잭션 팩토리 생성
	txFactory := tb.createTxFactory()

	// 배치 트랜잭션의 경우 가스 한도 조정
	batchGasLimit := tb.gasLimit * uint64(len(msgs))
	txFactory = txFactory.WithGas(batchGasLimit)

	// 서명되지 않은 트랜잭션 빌드
	txBuilder, err := txFactory.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to build batch transaction: %w", err)
	}

	// 트랜잭션 서명
	err = tx.Sign(ctx, txFactory, tb.config.KeyName(), txBuilder, true)
	if err != nil {
		return nil, fmt.Errorf("failed to sign batch transaction: %w", err)
	}

	// 트랜잭션 인코딩
	txBytes, err := tb.clientCtx.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("failed to encode batch transaction: %w", err)
	}

	tb.logger.Info("batch transaction built successfully",
		"message_count", len(msgs),
		"gas_limit", batchGasLimit,
		"sequence", txFactory.Sequence())

	return txBytes, nil
}

// GetTransactionSize는 트랜잭션 크기를 반환 (바이트)
func (tb *TransactionBuilder_impl) GetTransactionSize(ctx context.Context, result *scheduler.JobResult) (int, error) {
	txBytes, err := tb.BuildSubmitTx(ctx, result)
	if err != nil {
		return 0, err
	}

	return len(txBytes), nil
}

// ValidateTransaction은 트랜잭션의 유효성을 검증
func (tb *TransactionBuilder_impl) ValidateTransaction(ctx context.Context, result *scheduler.JobResult) error {
	if result == nil {
		return fmt.Errorf("job result cannot be nil")
	}

	if !result.Success {
		return fmt.Errorf("cannot validate transaction for failed job result")
	}

	if result.RequestID == 0 {
		return fmt.Errorf("invalid request ID: %d", result.RequestID)
	}

	if result.Data == "" {
		return fmt.Errorf("empty data in job result")
	}

	if result.Nonce == 0 {
		return fmt.Errorf("invalid nonce: %d", result.Nonce)
	}

	// 시퀀스 번호 유효성 확인
	currentSeq := tb.sequenceManager.GetSequence()
	if currentSeq == 0 {
		tb.logger.Warn("sequence is 0, may need synchronization")
	}

	// 가스 가격 유효성 확인
	if tb.gasPrices.IsZero() {
		return fmt.Errorf("gas prices not set")
	}

	// 계정 잔액은 트랜잭션 제출 시 자동으로 검증됨
	return nil
}

// GetEstimatedFee는 예상 수수료를 계산
func (tb *TransactionBuilder_impl) GetEstimatedFee(ctx context.Context, result *scheduler.JobResult) (sdk.Coins, error) {
	estimatedGas, err := tb.EstimateGas(ctx, result)
	if err != nil {
		// 가스 추정 실패 시 기본 가스 한도 사용
		estimatedGas = tb.gasLimit
	}

	// 수수료 계산: gasPrice * gasLimit
	fees := make(sdk.Coins, 0, len(tb.gasPrices))
	for _, gasPrice := range tb.gasPrices {
		feeAmount := gasPrice.Amount.MulInt64(int64(estimatedGas))
		fee := sdk.NewCoin(gasPrice.Denom, feeAmount.TruncateInt())
		fees = fees.Add(fee)
	}

	return fees, nil
}

// UpdateConfig는 설정을 업데이트
func (tb *TransactionBuilder_impl) UpdateConfig(cfg *config.Config) error {
	tb.config = cfg
	tb.gasAdjustment = cfg.GasAdjustment()
	tb.gasLimit = cfg.GasLimit()

	// 가스 가격 업데이트
	return tb.UpdateGasPrice(cfg.GasPrices())
}
