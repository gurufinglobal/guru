package app

import (
	"github.com/cosmos/cosmos-sdk/codec"
	precompiletypes "github.com/cosmos/evm/precompiles/types"
	"github.com/ethereum/go-ethereum/common"
	ethvm "github.com/ethereum/go-ethereum/core/vm"
)

func defaultStaticPrecompiles(
	keepers *AppKeepers,
	appCodec codec.Codec,
) map[common.Address]ethvm.PrecompiledContract {
	return precompiletypes.DefaultStaticPrecompiles(
		*keepers.StakingKeeper,
		keepers.DistrKeeper,
		keepers.BankKeeper,
		&keepers.ERC20Keeper,
		&keepers.TransferKeeper,
		keepers.IBCKeeper.ChannelKeeper,
		keepers.GovKeeper,
		keepers.SlashingKeeper,
		appCodec,
	)
}
