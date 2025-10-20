package testdata

import (
	contractutils "github.com/gurufinglobal/guru/v2/contracts/utils"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
)

func LoadSlashingCallerContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("SlashingCaller.json")
}
