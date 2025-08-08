package contracts

import (
	contractutils "github.com/GPTx-global/guru-v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/x/vm/types"
)

func LoadGovCallerContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("GovCaller.json")
}
