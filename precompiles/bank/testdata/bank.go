package testdata

import (
	contractutils "github.com/GPTx-global/guru-v2/v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/v2/x/vm/types"
)

func LoadBankCallerContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("BankCaller.json")
}
