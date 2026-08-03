package ante

import (
	"testing"

	cryptomultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmante "github.com/cosmos/evm/ante"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/stretchr/testify/require"
)

func TestSigVerificationGasConsumerPreservesNestedEthereumKeyCost(t *testing.T) {
	privateKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	pubKey := privateKey.PubKey()
	multiPubKey := cryptomultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{pubKey})
	multiSignature := multisigData(1, []signing.SignatureData{&signing.SingleSignatureData{}}, 0)
	meter := storetypes.NewInfiniteGasMeter()

	err = SigVerificationGasConsumer(meter, signing.SignatureV2{
		PubKey: multiPubKey,
		Data:   multiSignature,
	}, authtypes.DefaultParams())

	require.NoError(t, err)
	require.Equal(t, evmante.Secp256k1VerifyCost, uint64(meter.GasConsumed()))
}

func TestSigVerificationGasConsumerRejectsMalformedMultisig(t *testing.T) {
	pubKey := threeKeyMultisig(t)
	nestedPubKey := cryptomultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{pubKey})

	for _, test := range []struct {
		name    string
		pubKey  cryptotypes.PubKey
		data    *signing.MultiSignatureData
		wantErr string
	}{
		{
			name:    "oversized bit array",
			pubKey:  pubKey,
			data:    multisigData(4, nil, 3),
			wantErr: "bit array size is incorrect",
		},
		{
			name:    "missing signature",
			pubKey:  pubKey,
			data:    multisigData(3, nil, 0),
			wantErr: "signature size is incorrect",
		},
		{
			name:   "malformed nested multisig",
			pubKey: nestedPubKey,
			data: multisigData(1, []signing.SignatureData{
				multisigData(4, nil, 3),
			}, 0),
			wantErr: "bit array size is incorrect",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			meter := storetypes.NewInfiniteGasMeter()
			err := SigVerificationGasConsumer(meter, signing.SignatureV2{
				PubKey: test.pubKey,
				Data:   test.data,
			}, authtypes.DefaultParams())

			require.ErrorContains(t, err, test.wantErr)
			require.Zero(t, meter.GasConsumed())
		})
	}
}

func multisigData(size int, signatures []signing.SignatureData, signed ...int) *signing.MultiSignatureData {
	data := &signing.MultiSignatureData{
		BitArray:   cryptotypes.NewCompactBitArray(size),
		Signatures: signatures,
	}
	for _, index := range signed {
		data.BitArray.SetIndex(index, true)
	}
	return data
}

func threeKeyMultisig(t *testing.T) *cryptomultisig.LegacyAminoPubKey {
	t.Helper()
	pubKeys := make([]cryptotypes.PubKey, 3)
	for i := range pubKeys {
		privateKey, err := ethsecp256k1.GenerateKey()
		require.NoError(t, err)
		pubKeys[i] = privateKey.PubKey()
	}
	return cryptomultisig.NewLegacyAminoPubKey(2, pubKeys)
}
