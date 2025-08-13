package oracle

import (
	"testing"

	"github.com/GPTx-global/guru-v2/x/oracle/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func setupHandlerTest(t *testing.T) (sdk.Context, *keeper.Keeper) {
	ctx, k := setupTest(t)
	k.SetModeratorAddress(ctx, "guru1h9y8h0rh6tqxrj045fyvarnnyyxdg07693zkft")
	return ctx, k
}

// TestNewHandler disabled temporarily due to store setup issues
// func TestNewHandler(t *testing.T) {
// 	// Test implementation will be added later with proper integration test setup
// }
