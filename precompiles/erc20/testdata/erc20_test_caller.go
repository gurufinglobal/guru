package testdata

import (
	evmtypes "github.com/cosmos/evm/x/vm/types"
	contractutils "github.com/gurufinglobal/guru/v2/contracts/utils"
)

func LoadERC20TestCaller() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("ERC20TestCaller.json")
}
