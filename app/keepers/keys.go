package keepers

import (
	storetypes "cosmossdk.io/store/types"
	evidencetypes "cosmossdk.io/x/evidence/types"
	feegrant "cosmossdk.io/x/feegrant"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
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
		paramstypes.StoreKey,
		consensusparamtypes.StoreKey,
		upgradetypes.StoreKey,
		feegrant.StoreKey,
		evidencetypes.StoreKey,
		authzkeeper.StoreKey,
		// ibc keys
		ibcexported.StoreKey,
		ibctransfertypes.StoreKey,
		transwaptypes.StoreKey,
		// Cosmos EVM store keys
		evmtypes.StoreKey,
		feemarkettypes.StoreKey,
		erc20types.StoreKey,
		constitutiontypes.StoreKey,
		oracletypes.StoreKey,
		bextypes.StoreKey,
		feepolicytypes.StoreKey,
	)
	ak.tKeys = storetypes.NewTransientStoreKeys(
		paramstypes.TStoreKey,
		evmtypes.TransientKey,
		feemarkettypes.TransientKey,
	)
}

func (ak *AppKeepers) GetKVStoreKey(key string) *storetypes.KVStoreKey {
	return ak.kvKeys[key]
}

func (ak *AppKeepers) GetKVStoreKeys() map[string]*storetypes.KVStoreKey {
	return ak.kvKeys
}

func (ak *AppKeepers) GetTransientStoreKey(key string) *storetypes.TransientStoreKey {
	return ak.tKeys[key]
}

func (ak *AppKeepers) GetTransientStoreKeys() map[string]*storetypes.TransientStoreKey {
	return ak.tKeys
}
