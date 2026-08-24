package keeper

import (
	"context"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cryptoproto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestInitGenesisPanicsWhenBaseKeeperIsNil(t *testing.T) {
	k := &Keeper{}
	ctx := sdk.Context{}.WithContext(context.Background())

	require.Panics(t, func() {
		_ = k.InitGenesis(ctx, stakingtypes.DefaultGenesisState())
	})
}

func TestMergeValidatorUpdatesUsesBaseWhenNoOverrides(t *testing.T) {
	base := []abci.ValidatorUpdate{
		{
			PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: []byte{0x01}}},
			Power:  10,
		},
	}

	merged := mergeValidatorUpdates(base, nil)
	require.Equal(t, base, merged)
}

func TestMergeValidatorUpdatesAppliesOverrideByPubKey(t *testing.T) {
	base := []abci.ValidatorUpdate{
		{
			PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: []byte{0x01}}},
			Power:  10,
		},
		{
			PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: []byte{0x02}}},
			Power:  20,
		},
	}

	overrides := []abci.ValidatorUpdate{
		{
			PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: []byte{0x01}}},
			Power:  0,
		},
	}

	merged := mergeValidatorUpdates(base, overrides)
	require.Len(t, merged, 2)
	require.Equal(t, int64(0), merged[0].Power)
	require.Equal(t, int64(20), merged[1].Power)
}

func TestMergeValidatorUpdatesKeepsDifferentPubKeyTypes(t *testing.T) {
	base := []abci.ValidatorUpdate{
		{
			PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: []byte{0x01}}},
			Power:  10,
		},
	}

	overrides := []abci.ValidatorUpdate{
		{
			PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Secp256K1{Secp256K1: []byte{0x01}}},
			Power:  20,
		},
	}

	merged := mergeValidatorUpdates(base, overrides)
	require.Len(t, merged, 2)
	require.Equal(t, int64(10), merged[0].Power)
	require.Equal(t, int64(20), merged[1].Power)
}
