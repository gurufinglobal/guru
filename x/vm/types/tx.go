package types

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// type aliases for hexutil types used in EvmTxArgs conversion
type (
	hexutilBig    = hexutil.Big
	hexutilUint64 = hexutil.Uint64
	hexutilBytes  = hexutil.Bytes
)

// EvmTxArgs encapsulates all possible params to create all EVM txs types.
// This includes LegacyTx, DynamicFeeTx and AccessListTx
type EvmTxArgs struct {
	Nonce     uint64
	GasLimit  uint64
	Input     []byte
	GasFeeCap *big.Int
	GasPrice  *big.Int
	ChainID   *big.Int
	Amount    *big.Int
	GasTipCap *big.Int
	To        *common.Address
	Accesses  *ethtypes.AccessList
}

// ToTxData converts the EvmTxArgs into a *TransactionArgs
func (args *EvmTxArgs) ToTxData() *TransactionArgs {
	txArgs := &TransactionArgs{}

	if args.To != nil {
		to := *args.To
		txArgs.To = &to
	}

	if args.Amount != nil {
		val := (*hexutilBig)(args.Amount)
		txArgs.Value = val
	}

	if args.GasPrice != nil {
		gp := (*hexutilBig)(args.GasPrice)
		txArgs.GasPrice = gp
	}

	if args.GasFeeCap != nil {
		mfpg := (*hexutilBig)(args.GasFeeCap)
		txArgs.MaxFeePerGas = mfpg
	}

	if args.GasTipCap != nil {
		mppfg := (*hexutilBig)(args.GasTipCap)
		txArgs.MaxPriorityFeePerGas = mppfg
	}

	if args.ChainID != nil {
		cid := (*hexutilBig)(args.ChainID)
		txArgs.ChainID = cid
	}

	if args.Accesses != nil {
		txArgs.AccessList = args.Accesses
	}

	gas := hexutilUint64(args.GasLimit)
	txArgs.Gas = &gas

	nonce := hexutilUint64(args.Nonce)
	txArgs.Nonce = &nonce

	if args.Input != nil {
		input := hexutilBytes(args.Input)
		txArgs.Input = &input
	}

	return txArgs
}

// GetTxPriority returns the priority of a given Ethereum tx. It relies of the
// priority reduction global variable to calculate the tx priority given the tx
// tip price:
//
//	tx_priority = tip_price / priority_reduction
func GetTxPriority(tx *ethtypes.Transaction, baseFee *big.Int) (priority int64) {
	// calculate effective gas price
	tipPrice := tx.GasPrice()
	if baseFee != nil {
		tip, _ := tx.EffectiveGasTip(baseFee)
		tipPrice = tip
	}

	priority = math.MaxInt64
	priorityBig := new(big.Int).Quo(tipPrice, DefaultPriorityReduction.BigInt())

	// safety check
	if priorityBig.IsInt64() {
		priority = priorityBig.Int64()
	}

	return priority
}

// Failed returns if the contract execution failed in vm errors
func (m *MsgEthereumTxResponse) Failed() bool {
	return len(m.VmError) > 0
}

// Return is a helper function to help caller distinguish between revert reason
// and function return. Return returns the data after execution if no error occurs.
func (m *MsgEthereumTxResponse) Return() []byte {
	if m.Failed() {
		return nil
	}
	return common.CopyBytes(m.Ret)
}

// Revert returns the concrete revert reason if the execution is aborted by `REVERT`
// opcode. Note the reason can be nil if no data supplied with revert opcode.
func (m *MsgEthereumTxResponse) Revert() []byte {
	if m.VmError != vm.ErrExecutionReverted.Error() {
		return nil
	}
	return common.CopyBytes(m.Ret)
}
