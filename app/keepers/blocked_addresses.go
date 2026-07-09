package keepers

import (
	"fmt"
	"sort"

	"cosmossdk.io/core/address"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	corevm "github.com/ethereum/go-ethereum/core/vm"
)

func blockedBankAddresses(moduleAccountPerms map[string][]string, addressCodec address.Codec) map[string]bool {
	blockedAddrs := make(map[string]bool)
	addModuleAccountBlockedAddresses(blockedAddrs, moduleAccountPerms, addressCodec)
	addPrecompileBlockedAddresses(blockedAddrs, addressCodec)
	return blockedAddrs
}

func addModuleAccountBlockedAddresses(blockedAddrs map[string]bool, moduleAccountPerms map[string][]string, addressCodec address.Codec) {
	modules := make([]string, 0, len(moduleAccountPerms))
	for module := range moduleAccountPerms {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	for _, module := range modules {
		moduleAddress := authtypes.NewModuleAddress(module)
		moduleAddressString, err := addressCodec.BytesToString(moduleAddress)
		if err != nil {
			panic(fmt.Errorf("failed to convert module address to string: %v", err))
		}
		blockedAddrs[moduleAddressString] = true
	}
}

func addPrecompileBlockedAddresses(blockedAddrs map[string]bool, addressCodec address.Codec) {
	// Bank sends to EVM precompiles are blocked because these addresses are
	// executable endpoints, not normal account-controlled balances.
	blockedPrecompilesHex := append([]string{}, evmtypes.AvailableStaticPrecompiles...)
	for _, addr := range corevm.PrecompiledAddressesPrague {
		blockedPrecompilesHex = append(blockedPrecompilesHex, addr.Hex())
	}

	for _, precompile := range blockedPrecompilesHex {
		precompileAddress := common.HexToAddress(precompile)
		precompileAddressString, err := addressCodec.BytesToString(precompileAddress.Bytes())
		if err != nil {
			panic(fmt.Errorf("failed to convert precompile address to string: %v", err))
		}
		blockedAddrs[precompileAddressString] = true
	}
}
