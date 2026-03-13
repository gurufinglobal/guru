package contracts

import (
	evmtypes "github.com/cosmos/evm/x/vm/types"
	contractutils "github.com/gurufinglobal/guru/v2/contracts/utils"
)

func LoadDistributionCallerContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("DistributionCaller.json")
}
