package keepers

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmaddress "github.com/cosmos/evm/encoding/address"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
)

func TestBlockedBankAddressesIncludesModuleAccountsAndPrecompiles(t *testing.T) {
	addressCodec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)

	blocked := blockedBankAddresses(map[string][]string{
		authtypes.FeeCollectorName: nil,
	}, addressCodec)

	feeCollector, err := addressCodec.BytesToString(authtypes.NewModuleAddress(authtypes.FeeCollectorName))
	require.NoError(t, err)
	require.True(t, blocked[feeCollector])

	precompile, err := addressCodec.BytesToString(common.HexToAddress(evmtypes.AvailableStaticPrecompiles[0]).Bytes())
	require.NoError(t, err)
	require.True(t, blocked[precompile])
}
