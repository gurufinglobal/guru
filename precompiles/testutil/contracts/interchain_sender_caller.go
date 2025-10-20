package contracts

import (
	contractutils "github.com/gurufinglobal/guru/v2/contracts/utils"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
)

func LoadInterchainSenderCallerContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("InterchainSenderCaller.json")
}
