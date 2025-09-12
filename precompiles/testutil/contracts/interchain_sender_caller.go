package contracts

import (
	contractutils "github.com/GPTx-global/guru-v2/v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/v2/x/vm/types"
)

func LoadInterchainSenderCallerContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("InterchainSenderCaller.json")
}
