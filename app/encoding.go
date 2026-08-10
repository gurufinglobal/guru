package app

import (
	txsigning "cosmossdk.io/x/tx/signing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/gurufinglobal/guru/v2/config"
)

// EncodingConfig is the complete Stage A serialization boundary. It registers
// only Cosmos SDK standard types; application and EVM modules are absent.
type EncodingConfig struct {
	LegacyAmino       *codec.LegacyAmino
	InterfaceRegistry codectypes.InterfaceRegistry
	Codec             codec.Codec
	TxConfig          client.TxConfig
}

// MakeEncodingConfig creates codecs and a transaction configuration that share
// the same Guru Bech32-aware signing context.
func MakeEncodingConfig() (EncodingConfig, error) {
	signingOptions := txsigning.Options{
		FileResolver:          gogoproto.HybridResolver,
		AddressCodec:          address.NewBech32Codec(config.Bech32PrefixAccAddr),
		ValidatorAddressCodec: address.NewBech32Codec(config.Bech32PrefixValAddr),
	}

	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     gogoproto.HybridResolver,
		SigningOptions: signingOptions,
	})
	if err != nil {
		return EncodingConfig{}, err
	}

	legacyAmino := codec.NewLegacyAmino()
	std.RegisterLegacyAminoCodec(legacyAmino)
	std.RegisterInterfaces(interfaceRegistry)

	appCodec := codec.NewProtoCodec(interfaceRegistry)
	txConfig, err := authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{
		EnabledSignModes: authtx.DefaultSignModes,
		SigningOptions:   &signingOptions,
		SigningContext:   interfaceRegistry.SigningContext(),
	})
	if err != nil {
		return EncodingConfig{}, err
	}

	return EncodingConfig{
		LegacyAmino:       legacyAmino,
		InterfaceRegistry: interfaceRegistry,
		Codec:             appCodec,
		TxConfig:          txConfig,
	}, nil
}
