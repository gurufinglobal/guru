package types_test

import (
	"testing"

	"github.com/gurufinglobal/guru/v2/x/precisebank/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestFractionalBalanceKey(t *testing.T) {
	addr := sdk.AccAddress([]byte("test-address"))

	key := types.FractionalBalanceKey(addr)
	require.Equal(t, addr.Bytes(), key)
	require.Equal(t, addr, sdk.AccAddress(key), "key should be able to be converted back to address")
}
