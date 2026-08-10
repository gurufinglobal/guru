package app

import (
	storetypes "cosmossdk.io/store/types"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
)

type storeKeys struct {
	kv        map[string]*storetypes.KVStoreKey
	transient map[string]*storetypes.TransientStoreKey
}

func newStoreKeys() storeKeys {
	return storeKeys{
		kv: storetypes.NewKVStoreKeys(
			authtypes.StoreKey,
			banktypes.StoreKey,
			stakingtypes.StoreKey,
			minttypes.StoreKey,
			distrtypes.StoreKey,
			slashingtypes.StoreKey,
			govtypes.StoreKey,
			consensusparamtypes.StoreKey,
			upgradetypes.StoreKey,
			feegrant.StoreKey,
			evidencetypes.StoreKey,
			authzkeeper.StoreKey,
			ibcexported.StoreKey,
			evmtypes.StoreKey,
			feemarkettypes.StoreKey,
		),
		transient: storetypes.NewTransientStoreKeys(
			evmtypes.TransientKey,
			feemarkettypes.TransientKey,
		),
	}
}

func (keys storeKeys) kvKey(name string) *storetypes.KVStoreKey {
	return keys.kv[name]
}

func (keys storeKeys) transientKey(name string) *storetypes.TransientStoreKey {
	return keys.transient[name]
}
