package cmd

import (
	"testing"

	evmaddress "github.com/cosmos/evm/encoding/address"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
)

func TestParseEVMSendAddressRejectsMalformedRecipient(t *testing.T) {
	codec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)

	for _, raw := range []string{"not-an-address", "0x1234"} {
		_, err := parseEVMSendAddress(codec, raw)
		require.Error(t, err)
	}
}

func TestParseEVMSendAddressAcceptsBech32AndHex(t *testing.T) {
	codec := evmaddress.NewEvmCodec(appparams.Bech32PrefixAccAddr)
	bech32, err := codec.BytesToString([]byte("addr________________"))
	require.NoError(t, err)

	for _, raw := range []string{bech32, "0x1111111111111111111111111111111111111111"} {
		addr, err := parseEVMSendAddress(codec, raw)
		require.NoError(t, err)
		require.Len(t, addr, 20)
	}
}
