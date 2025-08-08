package testdata

import (
	contractutils "github.com/GPTx-global/guru-v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/x/vm/types"
)

func LoadCounterContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("Counter.json")
}

func LoadCounterFactoryContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("CounterFactory.json")
}
