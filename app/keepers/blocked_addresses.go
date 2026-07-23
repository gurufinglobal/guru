package keepers

import (
	"fmt"
	"sort"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		addBlockedAddress(blockedAddrs, addressCodec, moduleAddress)
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
		addBlockedAddress(blockedAddrs, addressCodec, precompileAddress.Bytes())
	}
}

func addBlockedAddress(blockedAddrs map[string]bool, addressCodec address.Codec, addressBytes []byte) {
	applicationAddress, err := addressCodec.BytesToString(addressBytes)
	if err != nil {
		panic(fmt.Errorf("failed to convert blocked address to application string: %v", err))
	}
	blockedAddrs[applicationAddress] = true

	// Cosmos SDK v0.54's BankKeeper.BlockedAddr still looks up addresses via
	// sdk.AccAddress.String(), which uses the process-global Bech32 prefix rather
	// than AccountKeeper's address codec. Register that representation as well so
	// an App constructed as a library has the same receive restrictions as gurud.
	blockedAddrs[sdk.AccAddress(addressBytes).String()] = true
}
