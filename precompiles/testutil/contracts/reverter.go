package contracts

import (
	contractutils "github.com/GPTx-global/guru-v2/v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/v2/x/vm/types"
)

func LoadReverterContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("Reverter.json")
}
