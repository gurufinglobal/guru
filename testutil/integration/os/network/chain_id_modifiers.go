//
// This files contains handler for the testing suite that has to be run to
// modify the chain configuration depending on the chainID

package network

import (
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	testconstants "github.com/gurufinglobal/guru/v2/testutil/constants"
)

// updateErc20GenesisStateForChainID modify the default genesis state for the
// bank module of the testing suite depending on the chainID.
func updateBankGenesisStateForChainID(bankGenesisState banktypes.GenesisState, coinInfo evmtypes.EvmCoinInfo) banktypes.GenesisState {
	metadata := generateBankGenesisMetadata(coinInfo)
	bankGenesisState.DenomMetadata = []banktypes.Metadata{metadata}

	return bankGenesisState
}

// generateBankGenesisMetadata generates the metadata
// for the Evm coin depending on the chainID.
func generateBankGenesisMetadata(coinInfo evmtypes.EvmCoinInfo) banktypes.Metadata {
	decimals := evmtypes.Decimals(coinInfo.Decimals)
	return banktypes.Metadata{
		Description: "The native EVM, governance and staking token of the Guru example chain",
		Base:        coinInfo.Denom,
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    coinInfo.Denom,
				Exponent: uint32(decimals), //#nosec G115 -- int overflow is not a concern here -- the conversion factor shouldn't be anything higher than 18.
			},
			{
				Denom:    coinInfo.ExtendedDenom,
				Exponent: 0,
			},
			{
				Denom:    coinInfo.DisplayDenom,
				Exponent: uint32(decimals),
			},
		},
		Name:    "Guru",
		Symbol:  "GXN",
		Display: coinInfo.DisplayDenom,
	}
}

// updateErc20GenesisStateForChainID modify the default genesis state for the
// erc20 module on the testing suite depending on the chainID.
func updateErc20GenesisStateForChainID(chainID testconstants.ChainID, erc20GenesisState erc20types.GenesisState) erc20types.GenesisState {
	erc20GenesisState.TokenPairs = updateErc20TokenPairs(chainID, erc20GenesisState.TokenPairs)

	return erc20GenesisState
}

// updateErc20TokenPairs modifies the erc20 token pairs to use the correct
// WGURU depending on ChainID
func updateErc20TokenPairs(chainID testconstants.ChainID, tokenPairs []erc20types.TokenPair) []erc20types.TokenPair {
	testnetAddress := GetWGURUContractHex(chainID)
	coinInfo := testconstants.ExampleChainCoinInfo[chainID]

	mainnetAddress := GetWGURUContractHex(testconstants.ExampleChainID)

	updatedTokenPairs := make([]erc20types.TokenPair, len(tokenPairs))
	for i, tokenPair := range tokenPairs {
		if tokenPair.Erc20Address == mainnetAddress {
			updatedTokenPairs[i] = erc20types.TokenPair{
				Erc20Address:  testnetAddress,
				Denom:         coinInfo.Denom,
				Enabled:       tokenPair.Enabled,
				ContractOwner: tokenPair.ContractOwner,
			}
		} else {
			updatedTokenPairs[i] = tokenPair
		}
	}
	return updatedTokenPairs
}
