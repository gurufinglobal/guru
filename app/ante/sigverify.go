package ante

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmante "github.com/cosmos/evm/ante"
)

var _ authante.SignatureVerificationGasConsumer = SigVerificationGasConsumer

// SigVerificationGasConsumer preserves Cosmos EVM's key-specific gas policy
// while applying the SDK's attacker-controlled multisig bounds checks.
func SigVerificationGasConsumer(
	meter storetypes.GasMeter,
	sig signing.SignatureV2,
	params authtypes.Params,
) error {
	pubKey, ok := sig.PubKey.(multisig.PubKey)
	if !ok {
		return evmante.SigVerificationGasConsumer(meter, sig, params)
	}

	multiSignature, ok := sig.Data.(*signing.MultiSignatureData)
	if !ok {
		return fmt.Errorf("expected %T, got, %T", &signing.MultiSignatureData{}, sig.Data)
	}

	return consumeMultisignatureVerificationGas(meter, multiSignature, pubKey, params, sig.Sequence)
}

func consumeMultisignatureVerificationGas(
	meter storetypes.GasMeter,
	sig *signing.MultiSignatureData,
	pubKey multisig.PubKey,
	params authtypes.Params,
	accountSequence uint64,
) error {
	size := sig.BitArray.Count()
	pubKeys := pubKey.GetPubKeys()
	if len(pubKeys) != size {
		return fmt.Errorf("bit array size is incorrect, expecting: %d", len(pubKeys))
	}

	sigIndex := 0
	for i := range size {
		if !sig.BitArray.GetIndex(i) {
			continue
		}
		if sigIndex >= len(sig.Signatures) {
			return fmt.Errorf("signature size is incorrect %d", len(sig.Signatures))
		}

		if err := SigVerificationGasConsumer(meter, signing.SignatureV2{
			PubKey:   pubKeys[i],
			Data:     sig.Signatures[sigIndex],
			Sequence: accountSequence,
		}, params); err != nil {
			return err
		}
		sigIndex++
	}

	return nil
}
