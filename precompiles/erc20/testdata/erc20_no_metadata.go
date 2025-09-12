package testdata

import (
	contractutils "github.com/GPTx-global/guru-v2/v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/v2/x/vm/types"
)

func LoadERC20NoMetadataContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("ERC20NoMetadata.json")
}
