package submiter

import (
	"context"
	"time"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"

	guruconfig "github.com/gurufinglobal/guru/v2/cmd/gurud/config"
	"github.com/gurufinglobal/guru/v2/oralce/config"
	"github.com/gurufinglobal/guru/v2/oralce/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

type Submitter struct {
	logger    log.Logger
	clientCtx client.Context
	accountN  uint64
	sequenceN uint64
}

func NewSubmitter(logger log.Logger, clientCtx client.Context) *Submitter {
	acc, seq, err := clientCtx.AccountRetriever.GetAccountNumberSequence(clientCtx, clientCtx.GetFromAddress())
	if err != nil {
		panic(err)
	}

	return &Submitter{
		logger:    logger,
		clientCtx: clientCtx,
		accountN:  acc,
		sequenceN: seq,
	}
}

// BroadcastTxWithRetry submits Oracle results to blockchain with automatic retry
// Handles various transaction errors and sequence number management
func (s *Submitter) BroadcastTxWithRetry(ctx context.Context, jobResult types.OracleJobResult) {
	maxAttempts := max(1, config.RetryMaxAttempts())
	for attempt := 0; attempt < maxAttempts; attempt++ {
		factory, txBuilder := s.buildTransaction(jobResult)
		if txBuilder == nil {
			s.logger.Error("failed to build tx", "attempt", attempt)
			return
		}

		txBytes := s.signTransaction(ctx, factory, txBuilder)
		if txBytes == nil {
			s.logger.Error("failed to sign tx", "attempt", attempt)
			return
		}

		res, err := s.clientCtx.BroadcastTx(txBytes)
		if err != nil {
			s.logger.Error("broadcast network error", "attempt", attempt+1, "max_attempts", maxAttempts, "error", err)
			time.Sleep(time.Second)
			continue
		}

		if res.Code == 0 {
			s.logger.Info("broadcast success", "tx_hash", res.TxHash)
			s.sequenceN++
			return
		}

		switch res.Code {
		case 18:
			s.logger.Info("already certified", "attempt", attempt+1, "max_attempts", maxAttempts)
			return
		case 32:
			failedSeq := s.sequenceN
			_, s.sequenceN, err = s.clientCtx.AccountRetriever.GetAccountNumberSequence(s.clientCtx, s.clientCtx.GetFromAddress())
			if err != nil {
				s.logger.Warn("failed to get account number and sequence", "error", err)
				return
			}

			s.logger.Info("sequence number rolled back", "failed_seq", failedSeq, "new_seq", s.sequenceN)
			time.Sleep(time.Second)
			continue
		default:
			s.logger.Error("unexpected error code", "attempt", attempt+1, "max_attempts", maxAttempts, "code", res.Code, "raw_log", res.RawLog)
			return
		}
	}

	s.logger.Info("failed to broadcast tx after max attempts", "max_attempts", maxAttempts)
}

// buildTransaction creates an unsigned transaction for Oracle data submission
// Configures all transaction parameters including gas, fees, and message data
func (s *Submitter) buildTransaction(jobResult types.OracleJobResult) (tx.Factory, client.TxBuilder) {
	gasPrice, err := sdk.ParseDecCoin(config.GasPrices() + guruconfig.BaseDenom)
	if err != nil {
		s.logger.Error("failed to parse gas price", "error", err)
		return tx.Factory{}, nil
	}

	factory := tx.Factory{}.
		WithTxConfig(s.clientCtx.TxConfig).
		WithAccountRetriever(s.clientCtx.AccountRetriever).
		WithKeybase(s.clientCtx.Keyring).
		WithChainID(config.ChainID()).
		WithGas(config.GasLimit()).
		WithGasAdjustment(config.GasAdjustment()).
		WithGasPrices(gasPrice.String()).
		WithAccountNumber(s.accountN).
		WithSequence(s.sequenceN).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	msg := &oracletypes.MsgSubmitOracleData{
		AuthorityAddress: s.clientCtx.GetFromAddress().String(),
		DataSet: &oracletypes.SubmitDataSet{
			RequestId: jobResult.ID,
			RawData:   jobResult.Data,
			Nonce:     jobResult.Nonce,
			Provider:  s.clientCtx.GetFromAddress().String(),
			Signature: nil,
		},
	}

	signBytes, err := msg.DataSet.Bytes()
	if err != nil {
		s.logger.Error("failed to get sign bytes", "error", err)
		return tx.Factory{}, nil
	}

	signature, _, err := s.clientCtx.Keyring.Sign(config.KeyName(), signBytes, factory.SignMode())
	if err != nil {
		s.logger.Error("failed to sign tx", "error", err)
		return tx.Factory{}, nil
	}
	msg.DataSet.Signature = signature

	txBuilder, err := factory.BuildUnsignedTx(msg)
	if err != nil {
		s.logger.Error("failed to build unsigned tx", "error", err)
		return tx.Factory{}, nil
	}

	return factory, txBuilder
}

// signTransaction signs the transaction and encodes it for broadcast
// Returns binary-encoded transaction bytes ready for blockchain submission
func (s *Submitter) signTransaction(ctx context.Context, factory tx.Factory, txBuilder client.TxBuilder) []byte {
	if err := tx.Sign(ctx, factory, config.KeyName(), txBuilder, true); err != nil {
		s.logger.Error("failed to sign tx", "error", err)
		return nil
	}

	txBytes, err := s.clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		s.logger.Error("failed to encode tx", "error", err)
		return nil
	}

	return txBytes
}
