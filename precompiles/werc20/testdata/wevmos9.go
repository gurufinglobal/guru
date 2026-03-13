package testdata

import (
	evmtypes "github.com/cosmos/evm/x/vm/types"
	contractutils "github.com/gurufinglobal/guru/v2/contracts/utils"
)

// LoadWGURU9Contract load the WGURU9 contract from the json representation of
// the Solidity contract.
func LoadWGURU9Contract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("WGURU9.json")
}
