package app

import (
	"fmt"
	"sort"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethvm "github.com/ethereum/go-ethereum/core/vm"
)

func moduleAccountPermissions() map[string][]string {
	return map[string][]string{
		authtypes.FeeCollectorName:     nil,
		distrtypes.ModuleName:          nil,
		minttypes.ModuleName:           {authtypes.Minter},
		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		govtypes.ModuleName:            {authtypes.Burner},
		evmtypes.ModuleName:            {authtypes.Minter, authtypes.Burner},
		feemarkettypes.ModuleName:      nil,
	}
}

func blockedBankAddresses(modulePermissions map[string][]string, addressCodec address.Codec) (map[string]bool, error) {
	blocked := make(map[string]bool)

	modules := make([]string, 0, len(modulePermissions))
	for moduleName := range modulePermissions {
		modules = append(modules, moduleName)
	}
	sort.Strings(modules)
	for _, moduleName := range modules {
		if err := addBlockedAddress(blocked, addressCodec, authtypes.NewModuleAddress(moduleName)); err != nil {
			return nil, err
		}
	}

	reservedPrecompiles := append([]string(nil), evmtypes.AvailableStaticPrecompiles...)
	for _, precompile := range ethvm.PrecompiledAddressesPrague {
		reservedPrecompiles = append(reservedPrecompiles, precompile.Hex())
	}
	for _, precompile := range reservedPrecompiles {
		if err := addBlockedAddress(blocked, addressCodec, common.HexToAddress(precompile).Bytes()); err != nil {
			return nil, err
		}
	}

	return blocked, nil
}

func addBlockedAddress(blocked map[string]bool, addressCodec address.Codec, addressBytes []byte) error {
	applicationAddress, err := addressCodec.BytesToString(addressBytes)
	if err != nil {
		return fmt.Errorf("encode blocked address: %w", err)
	}
	blocked[applicationAddress] = true
	blocked[sdk.AccAddress(addressBytes).String()] = true
	return nil
}
