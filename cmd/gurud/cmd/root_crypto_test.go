package cmd

import (
	"testing"

	cryptoutil "github.com/cosmos/cosmos-sdk/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/stretchr/testify/require"
)

func TestCryptoKeyArmorCodecRoundTripsSupportedKeys(t *testing.T) {
	initCryptoKeyArmorCodec()

	ethKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	sdkKey := secp256k1.GenPrivKey()

	tests := []struct {
		name string
		algo string
		key  cryptotypes.PrivKey
	}{
		{name: "sdk secp256k1", algo: sdkKey.Type(), key: sdkKey},
		{name: "evm eth_secp256k1", algo: ethsecp256k1.KeyType, key: ethKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const password = "correct-password"
			armored := cryptoutil.EncryptArmorPrivKey(tc.key, password, tc.algo)

			decoded, algo, err := cryptoutil.UnarmorDecryptPrivKey(armored, password)
			require.NoError(t, err)
			require.Equal(t, tc.algo, algo)
			require.True(t, tc.key.Equals(decoded))

			_, _, err = cryptoutil.UnarmorDecryptPrivKey(armored, "wrong-password")
			require.Error(t, err)
		})
	}
}
