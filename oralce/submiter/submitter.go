// Package submiter handles Oracle result submission to the blockchain
// Manages transaction building, signing, and broadcasting with retry logic
package submiter

import (
	"context"
	"time"

	"cosmossdk.io/log"
	guruconfig "github.com/GPTx-global/guru-v2/cmd/gurud/config"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

// Submitter manages transaction submission for Oracle results
// Handles account sequence tracking and transaction retry logic
type Submitter struct {
	logger    log.Logger
	baseCtx   context.Context
	clientCtx client.Context
	accountN  uint64 // Account number for transaction signing
	sequenceN uint64 // Current sequence number for transaction ordering
}

// NewSubmitter creates a new transaction submitter with current account state
// Initializes account number and sequence for proper transaction ordering
func NewSubmitter(logger log.Logger, baseCtx context.Context, clientCtx client.Context) *Submitter {
	// Get current account number and sequence from blockchain
	acc, seq, err := clientCtx.AccountRetriever.GetAccountNumberSequence(clientCtx, clientCtx.GetFromAddress())
	if err != nil {
		panic(err)
	}

	return &Submitter{
		logger:    logger,
		baseCtx:   baseCtx,
		clientCtx: clientCtx,
		accountN:  acc,
		sequenceN: seq,
	}
}

// BroadcastTxWithRetry submits Oracle results to blockchain with automatic retry
// Handles various transaction errors and sequence number management
func (s *Submitter) BroadcastTxWithRetry(jobResult types.OracleJobResult) {
	maxAttempts := max(1, config.RetryMaxAttempts())
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Build transaction for Oracle result submission
		factory, txBuilder := s.buildTransaction(jobResult)
		if txBuilder == nil {
			s.logger.Error("failed to build tx", "attempt", attempt)
			return
		}

		// Sign the transaction
		txBytes := s.signTransaction(factory, txBuilder)
		if txBytes == nil {
			s.logger.Error("failed to sign tx", "attempt", attempt)
			return
		}

		// Broadcast transaction to blockchain
		res, err := s.clientCtx.BroadcastTx(txBytes)
		if err != nil {
			retryDelay := time.Duration(1*attempt) * time.Second
			s.logger.Error("broadcast network error", "attempt", attempt+1, "max_attempts", maxAttempts, "error", err, "retry_delay", retryDelay)
			time.Sleep(retryDelay)
			continue
		}

		// Handle successful transaction
		if res.Code == 0 {
			s.logger.Info("broadcast success", "tx_hash", res.TxHash)
			s.sequenceN++
			return
		}

		// Handle specific error codes
		switch res.Code {
		case 18:
			// Data already certified, no retry needed
			s.logger.Info("already certified", "attempt", attempt+1, "max_attempts", maxAttempts)
			return
		case 32:
			// Sequence mismatch, refresh and retry
			failedSeq := s.sequenceN
			_, s.sequenceN, err = s.clientCtx.AccountRetriever.GetAccountNumberSequence(s.clientCtx, s.clientCtx.GetFromAddress())
			if err != nil {
				s.logger.Warn("failed to get account number and sequence", "error", err)
				return
			}

			s.logger.Info("sequence number rolled back", "failed_seq", failedSeq, "new_seq", s.sequenceN)
			retryDelay := time.Duration(1*attempt) * time.Second
			time.Sleep(retryDelay)
			continue
		default:
			// Unexpected error, stop retrying
			s.logger.Error("unexpected error code", "attempt", attempt+1, "max_attempts", maxAttempts, "code", res.Code, "raw_log", res.RawLog)
			return
		}
	}

	s.logger.Info("failed to broadcast tx after max attempts", "max_attempts", maxAttempts)
}

// buildTransaction creates an unsigned transaction for Oracle data submission
// Configures all transaction parameters including gas, fees, and message data
func (s *Submitter) buildTransaction(jobResult types.OracleJobResult) (tx.Factory, client.TxBuilder) {
	// Create Oracle data submission message
	msg := &oracletypes.MsgSubmitOracleData{
		AuthorityAddress: s.clientCtx.GetFromAddress().String(),
		DataSet: &oracletypes.SubmitDataSet{
			RequestId: jobResult.ID,
			RawData:   jobResult.Data,
			Nonce:     jobResult.Nonce,
			Provider:  s.clientCtx.GetFromAddress().String(),
			Signature: "signature",
		},
	}

	// Parse gas prices from configuration
	gasPrice, err := sdk.ParseDecCoin(config.GasPrices() + guruconfig.BaseDenom) 
	
	if err != nil {
		s.logger.Error("failed to parse gas price", "error", err)
		return tx.Factory{}, nil
	}

	// Configure transaction factory with all parameters
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

	// Build unsigned transaction
	txBuilder, err := factory.BuildUnsignedTx(msg)
	if err != nil {
		s.logger.Error("failed to build unsigned tx", "error", err)
		return tx.Factory{}, nil
	}

	return factory, txBuilder
}

// signTransaction signs the transaction and encodes it for broadcast
// Returns binary-encoded transaction bytes ready for blockchain submission
func (s *Submitter) signTransaction(factory tx.Factory, txBuilder client.TxBuilder) []byte {
	// Sign transaction with configured key
	if err := tx.Sign(s.baseCtx, factory, config.KeyName(), txBuilder, true); err != nil {
		s.logger.Error("failed to sign tx", "error", err)
		return nil
	}

	// Encode transaction to binary format for broadcast
	txBytes, err := s.clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		s.logger.Error("failed to encode tx", "error", err)
		return nil
	}

	return txBytes
}
