package gurud

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	transferkeeper "github.com/cosmos/ibc-go/v10/modules/apps/transfer/keeper"
	channelkeeper "github.com/cosmos/ibc-go/v10/modules/core/04-channel/keeper"

	evidencekeeper "cosmossdk.io/x/evidence/keeper"

	"github.com/cosmos/cosmos-sdk/codec"
	distributionkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	cmn "github.com/cosmos/evm/precompiles/common"
	precompiletypes "github.com/cosmos/evm/precompiles/types"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
)

// NewAvailableStaticPrecompiles returns the list of all available static precompiled contracts from Cosmos EVM.
//
// NOTE: this should only be used during initialization of the Keeper.
func NewAvailableStaticPrecompiles(
	stakingKeeper stakingkeeper.Keeper,
	distributionKeeper distributionkeeper.Keeper,
	bankKeeper cmn.BankKeeper,
	erc20Keeper erc20keeper.Keeper,
	transferKeeper transferkeeper.Keeper,
	channelKeeper *channelkeeper.Keeper,
	_ *evmkeeper.Keeper,
	govKeeper govkeeper.Keeper,
	slashingKeeper slashingkeeper.Keeper,
	_ evidencekeeper.Keeper,
	codec codec.Codec,
) map[common.Address]vm.PrecompiledContract {
	return precompiletypes.DefaultStaticPrecompiles(
		stakingKeeper,
		distributionKeeper,
		bankKeeper,
		&erc20Keeper,
		&transferKeeper,
		channelKeeper,
		govKeeper,
		slashingKeeper,
		codec,
	)
}
