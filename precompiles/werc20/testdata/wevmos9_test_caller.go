package testdata

import (
	contractutils "github.com/GPTx-global/guru-v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/x/vm/types"
)

func LoadWEVMOS9TestCaller() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("WEVMOS9TestCaller.json")
}
