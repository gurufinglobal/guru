package params

import (
	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

type KeepersInitConfig struct {
	AppCodec               codec.Codec
	BaseApp                *baseapp.BaseApp
	Logger                 log.Logger
	HomePath               string
	SkipUpgradeHeights     map[int64]bool
	AccountAddressPrefix   string
	ValidatorAddressPrefix string
	ConsensusAddressPrefix string
	EVMChainID             uint64
	EVMTracer              string
	ModuleAccountPerms     map[string][]string
}

func DefaultModuleAccountPermissions() map[string][]string {
	return map[string][]string{
		authtypes.FeeCollectorName:     nil,
		constitutiontypes.ModuleName:   {authtypes.Burner},
		distrtypes.ModuleName:          nil,
		ibctransfertypes.ModuleName:    {authtypes.Minter, authtypes.Burner},
		minttypes.ModuleName:           {authtypes.Minter},
		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		govtypes.ModuleName:            {authtypes.Burner},

		// Cosmos EVM modules
		evmtypes.ModuleName:       {authtypes.Minter, authtypes.Burner},
		feemarkettypes.ModuleName: nil,
		erc20types.ModuleName:     {authtypes.Minter, authtypes.Burner},
	}
}
