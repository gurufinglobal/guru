package testdata

import (
	contractutils "github.com/GPTx-global/guru-v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/x/vm/types"
)

func LoadERC20TestCaller() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("ERC20TestCaller.json")
}
