package keepers

import (
	"slices"
	"strings"

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
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
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
		constitutiontypes.StoreKey,
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
	names := make([]string, 0, len(ak.kvKeys)+len(ak.objKeys))
	for name := range ak.kvKeys {
		names = append(names, name)
	}
	for name := range ak.objKeys {
		names = append(names, name)
	}
	slices.SortStableFunc(names, func(a, b string) int {
		return strings.Compare(a, b)
	})

	nonTransientKeys := make([]storetypes.StoreKey, 0, len(names))
	for _, name := range names {
		if key, ok := ak.kvKeys[name]; ok {
			nonTransientKeys = append(nonTransientKeys, key)
			continue
		}
		if key, ok := ak.objKeys[name]; ok {
			nonTransientKeys = append(nonTransientKeys, key)
		}
	}
	return nonTransientKeys
}
