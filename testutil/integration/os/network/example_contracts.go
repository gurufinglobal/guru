package network

import (
	testconstants "github.com/gurufinglobal/guru/v2/testutil/constants"
)

// chainsWGURUHex is an utility map used to retrieve the WGURU contract
// address in hex format from the chain ID.
//
// TODO: refactor to define this in the example chain initialization and pass as function argument
var chainsWGURUHex = map[testconstants.ChainID]string{
	testconstants.ExampleChainID: testconstants.WGURUContractMainnet,
}

// GetWGURUContractHex returns the hex format of address for the WGURU contract
// given the chainID. If the chainID is not found, it defaults to the mainnet
// address.
func GetWGURUContractHex(chainID testconstants.ChainID) string {
	address, found := chainsWGURUHex[chainID]

	// default to mainnet address
	if !found {
		address = chainsWGURUHex[testconstants.ExampleChainID]
	}

	return address
}
