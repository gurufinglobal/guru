package ics20

import (
	"embed"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	channelkeeper "github.com/cosmos/ibc-go/v10/modules/core/04-channel/keeper"

	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	cmn "github.com/gurufinglobal/guru/v2/precompiles/common"
	transferkeeper "github.com/gurufinglobal/guru/v2/x/ibc/transfer/keeper"
	evmkeeper "github.com/gurufinglobal/guru/v2/x/vm/keeper"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
)

// PrecompileAddress of the ICS-20 EVM extension in hex format.
const PrecompileAddress = "0x0000000000000000000000000000000000000802"

var _ vm.PrecompiledContract = &Precompile{}

var (
	// Embed abi json file to the executable binary. Needed when importing as dependency.
	//
	//go:embed abi.json
	f   embed.FS
	ABI abi.ABI
)

func init() {
	var err error
	ABI, err = cmn.LoadABI(f, "abi.json")
	if err != nil {
		panic(err)
	}
}

type Precompile struct {
	cmn.Precompile

	abi.ABI
	stakingKeeper  stakingkeeper.Keeper
	transferKeeper transferkeeper.Keeper
	channelKeeper  *channelkeeper.Keeper
	evmKeeper      *evmkeeper.Keeper
}

// NewPrecompile creates a new ICS-20 Precompile instance as a
// PrecompiledContract interface.
func NewPrecompile(
	stakingKeeper stakingkeeper.Keeper,
	transferKeeper transferkeeper.Keeper,
	channelKeeper *channelkeeper.Keeper,
	evmKeeper *evmkeeper.Keeper,
	bankKeeper cmn.BankKeeper,
) *Precompile {
	return &Precompile{
		Precompile: cmn.Precompile{
			KvGasConfig:           storetypes.KVGasConfig(),
			TransientKVGasConfig:  storetypes.TransientGasConfig(),
			ContractAddress:       common.HexToAddress(evmtypes.ICS20PrecompileAddress),
			BalanceHandlerFactory: cmn.NewBalanceHandlerFactory(bankKeeper),
		},
		ABI:            ABI,
		transferKeeper: transferKeeper,
		channelKeeper:  channelKeeper,
		stakingKeeper:  stakingKeeper,
		evmKeeper:      evmKeeper,
	}
}

// RequiredGas calculates the precompiled contract's base gas rate.
func (p Precompile) RequiredGas(input []byte) uint64 {
	// NOTE: This check avoid panicking when trying to decode the method ID
	if len(input) < 4 {
		return 0
	}

	methodID := input[:4]

	method, err := p.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method))
}

func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) ([]byte, error) {
	return p.RunNativeAction(evm, contract, func(ctx sdk.Context) ([]byte, error) {
		return p.Execute(ctx, evm.StateDB, contract, readOnly)
	})
}

func (p Precompile) Execute(ctx sdk.Context, stateDB vm.StateDB, contract *vm.Contract, readOnly bool) ([]byte, error) {
	method, args, err := cmn.SetupABI(p.ABI, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}

	var bz []byte

	switch method.Name {
	// ICS20 transactions
	case TransferMethod:
		bz, err = p.Transfer(ctx, contract, stateDB, method, args)
	// ICS20 queries
	case DenomMethod:
		bz, err = p.Denom(ctx, contract, method, args)
	case DenomsMethod:
		bz, err = p.Denoms(ctx, contract, method, args)
	case DenomHashMethod:
		bz, err = p.DenomHash(ctx, contract, method, args)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}

	return bz, err
}

// IsTransaction checks if the given method name corresponds to a transaction or query.
//
// Available ics20 transactions are:
//   - Transfer
func (Precompile) IsTransaction(method *abi.Method) bool {
	switch method.Name {
	case TransferMethod:
		return true
	default:
		return false
	}
}
