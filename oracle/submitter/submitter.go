package submitter

import (
	"context"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/gurufinglobal/guru/v2/y/oracle/types"
)

type SubmitClient interface {
	BroadcastTx(txBytes []byte) (res *sdk.TxResponse, err error)
}

type Submitter struct {
	logger       log.Logger
	keyname      string
	txConfig     client.TxConfig
	accountInfo  *AccountInfo
	baseFactory  tx.Factory
	submitClient SubmitClient
}

func New(logger log.Logger, keyname string, txConfig client.TxConfig, accountInfo *AccountInfo, baseFactory tx.Factory, submitClient SubmitClient) *Submitter {
	return &Submitter{
		logger:       logger,
		keyname:      keyname,
		txConfig:     txConfig,
		accountInfo:  accountInfo,
		baseFactory:  baseFactory,
		submitClient: submitClient,
	}
}

func (s *Submitter) Start(ctx context.Context, resultCh <-chan oracletypes.OracleReport) {
	if err := s.accountInfo.ResetAccountInfo(ctx); err != nil {
		s.logger.Error("failed to reset account info", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case result, ok := <-resultCh:
			if !ok {
				s.logger.Info("result channel closed, stopping submitter")
				return
			}
			s.submit(ctx, result)
		}
	}
}

func (s *Submitter) UpdateGasPrice(gasPrice string) {
	s.baseFactory = s.baseFactory.WithGasPrices(gasPrice)
}

func (s *Submitter) submit(ctx context.Context, result oracletypes.OracleReport) {
	result.Provider = s.accountInfo.address.String()

	kr := s.baseFactory.Keybase()
	resultBytes, err := result.Bytes()
	if err != nil {
		s.logger.Error("failed to get result bytes", "error", err)
		return
	}

	signature, _, err := kr.Sign(s.keyname, resultBytes, s.baseFactory.SignMode())
	if err != nil {
		s.logger.Error("failed to sign result", "error", err)
		return
	}

	result.Signature = signature
	if err := result.ValidateBasic(); err != nil {
		s.logger.Error("invalid oracle report", "error", err)
		return
	}

	finalFactory := s.baseFactory.
		WithAccountNumber(s.accountInfo.AccountNumber()).
		WithSequence(s.accountInfo.CurrentSequenceNumber())

	txBuilder, err := finalFactory.BuildUnsignedTx(&result)
	if err != nil {
		s.logger.Error("failed to build unsigned tx", "error", err)
		return
	}

	if err := tx.Sign(ctx, finalFactory, s.keyname, txBuilder, true); err != nil {
		s.logger.Error("failed to sign tx", "error", err)
		return
	}

	txBytes, err := s.txConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		s.logger.Error("failed to encode tx", "error", err)
		return
	}

	res, err := s.submitClient.BroadcastTx(txBytes)
	if err != nil {
		s.logger.Error("failed to broadcast tx", "error", err)
		return
	}

	switch res.Code {
	case 0:
		s.logger.Info("tx broadcasted successfully",
			"tx_hash", res.TxHash,
			"request_id", result.RequestId,
			"nonce", result.Nonce,
			"sequence", s.accountInfo.CurrentSequenceNumber(),
		)
		s.accountInfo.IncrementSequenceNumber()
	case 18:
		s.logger.Info("tx already certified", "tx_hash", res.TxHash, "request_id", result.RequestId, "nonce", result.Nonce)
	case 32:
		if err := s.accountInfo.ResetAccountInfo(ctx); err != nil {
			s.logger.Error("failed to reset account info", "error", err)
		}
		s.logger.Info("tx sequence number rolled back", "tx_hash", res.TxHash, "request_id", result.RequestId, "nonce", result.Nonce)
	case 33:
		if err := s.accountInfo.ResetAccountInfo(ctx); err != nil {
			s.logger.Error("failed to reset account info", "error", err)
		}
		s.logger.Info("tx sequence number already used", "tx_hash", res.TxHash, "request_id", result.RequestId, "nonce", result.Nonce)
	default:
		s.logger.Error("unexpected error code",
			"code", res.Code,
			"raw_log", res.RawLog,
			"tx_hash", res.TxHash,
			"request_id", result.RequestId,
			"nonce", result.Nonce,
		)
	}
}
