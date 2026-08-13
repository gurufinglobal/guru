package staking

import (
	"context"
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	sdkstaking "github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	customkeeper "github.com/gurufinglobal/guru/v2/x/staking/keeper"
)

var (
	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
	_ module.HasABCIEndBlock    = AppModule{}
	_ module.HasABCIGenesis     = AppModule{}
)

type AppModule struct {
	sdkstaking.AppModule

	keeper *customkeeper.Keeper
}

func NewAppModule(baseModule sdkstaking.AppModule, keeper *customkeeper.Keeper) AppModule {
	return AppModule{
		AppModule: baseModule,
		keeper:    keeper,
	}
}

func (am AppModule) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	return am.keeper.EndBlocker(ctx)
}

// InitGenesis runs SDK staking initialization, then reapplies validator-set updates
// through the custom keeper path so the min self-bond filter is enforced from genesis.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	var genesisState stakingtypes.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)

	return am.keeper.InitGenesis(ctx, &genesisState)
}
