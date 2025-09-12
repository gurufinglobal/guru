package testdata

import (
	contractutils "github.com/GPTx-global/guru-v2/v2/contracts/utils"
	evmtypes "github.com/GPTx-global/guru-v2/v2/x/vm/types"
)

// LoadBytes32MetadataTokenContract loads the Bytes32MetadataToken contract
// from the compiled JSON data.
func LoadBytes32MetadataTokenContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("Bytes32MetadataToken.json")
}
