package keepers

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	feegrant "github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
)

func (ak *AppKeepers) GenerateKeys() {
	ak.kvKeys = storetypes.NewKVStoreKeys(
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
		// ibc keys
		ibcexported.StoreKey,
		ibctransfertypes.StoreKey,
		// Cosmos EVM store keys
		evmtypes.StoreKey,
		feemarkettypes.StoreKey,
		erc20types.StoreKey,
	)
	ak.objKeys = storetypes.NewObjectStoreKeys(
		banktypes.ObjectStoreKey,
		evmtypes.ObjectKey,
	)
}

func (ak *AppKeepers) GetKVStoreKey(key string) *storetypes.KVStoreKey {
	return ak.kvKeys[key]
}

func (ak *AppKeepers) GetKVStoreKeys() map[string]*storetypes.KVStoreKey {
	return ak.kvKeys
}

func (ak *AppKeepers) GetObjectStoreKey(key string) *storetypes.ObjectStoreKey {
	return ak.objKeys[key]
}

func (ak *AppKeepers) GetObjectStoreKeys() map[string]*storetypes.ObjectStoreKey {
	return ak.objKeys
}

func (ak *AppKeepers) GetNonTransientKeys() []storetypes.StoreKey {
	var nonTransientKeys []storetypes.StoreKey
	for _, key := range ak.kvKeys {
		nonTransientKeys = append(nonTransientKeys, key)
	}
	for _, key := range ak.objKeys {
		nonTransientKeys = append(nonTransientKeys, key)
	}
	return nonTransientKeys
}
