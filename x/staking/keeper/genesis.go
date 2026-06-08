package keeper

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	cryptoproto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// InitGenesis delegates base staking initialization and reapplies validator set
// updates through the custom path to enforce min self-bond at genesis.
func (k *Keeper) InitGenesis(ctx context.Context, data *stakingtypes.GenesisState) (res []abci.ValidatorUpdate) {
	baseUpdates := k.Keeper.InitGenesis(ctx, data)

	sdkCtx := sdk.UnwrapSDKContext(ctx).WithBlockHeight(1 - sdk.ValidatorUpdateDelay)
	updates, err := k.ApplyAndReturnValidatorSetUpdates(sdkCtx)
	if err != nil {
		panic(err)
	}

	return mergeValidatorUpdates(baseUpdates, updates)
}

func mergeValidatorUpdates(base []abci.ValidatorUpdate, overrides []abci.ValidatorUpdate) []abci.ValidatorUpdate {
	if len(overrides) == 0 {
		return base
	}

	merged := make(map[string]abci.ValidatorUpdate, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))

	add := func(updates []abci.ValidatorUpdate) {
		for _, update := range updates {
			key := validatorUpdateKey(update)
			if _, exists := merged[key]; !exists {
				order = append(order, key)
			}
			merged[key] = update
		}
	}

	add(base)
	add(overrides)

	out := make([]abci.ValidatorUpdate, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}

	return out
}

func validatorUpdateKey(update abci.ValidatorUpdate) string {
	switch pubKey := update.PubKey.GetSum().(type) {
	case *cryptoproto.PublicKey_Ed25519:
		return "ed25519:" + string(pubKey.Ed25519)
	case *cryptoproto.PublicKey_Secp256K1:
		return "secp256k1:" + string(pubKey.Secp256K1)
	case *cryptoproto.PublicKey_Bls12381:
		return "bls12381:" + string(pubKey.Bls12381)
	default:
		panic("unknown validator update public key type")
	}
}
