package app

import (
	"sync"

	txsigning "cosmossdk.io/x/tx/signing"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	evmaddress "github.com/cosmos/evm/encoding/address"
	evmcodec "github.com/cosmos/evm/encoding/codec"
	"github.com/cosmos/evm/ethereum/eip712"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/gurufinglobal/guru/v2/config"
)

var (
	encodingConfigOnce sync.Once
	encodingConfig     EncodingConfig
	encodingConfigErr  error
)

// EncodingConfig is the serialization boundary shared by the application and
// clients. It includes Cosmos EVM keys, custom signers, and EIP-712 support.
type EncodingConfig struct {
	LegacyAmino       *codec.LegacyAmino
	InterfaceRegistry codectypes.InterfaceRegistry
	Codec             codec.Codec
	TxConfig          client.TxConfig
}

// MakeEncodingConfig creates codecs and a transaction configuration that share
// the same Guru Bech32-aware signing context.
func MakeEncodingConfig() (EncodingConfig, error) {
	if err := config.SetupSDKConfig(); err != nil {
		return EncodingConfig{}, err
	}
	encodingConfigOnce.Do(func() {
		encodingConfig, encodingConfigErr = buildEncodingConfig()
	})
	return encodingConfig, encodingConfigErr
}

func buildEncodingConfig() (EncodingConfig, error) {
	signingOptions := txsigning.Options{
		FileResolver:          gogoproto.HybridResolver,
		AddressCodec:          evmaddress.NewEvmCodec(config.Bech32PrefixAccAddr),
		ValidatorAddressCodec: evmaddress.NewEvmCodec(config.Bech32PrefixValAddr),
		CustomGetSigners: map[protoreflect.FullName]txsigning.GetSignersFunc{
			evmtypes.MsgEthereumTxCustomGetSigner.MsgType:     evmtypes.MsgEthereumTxCustomGetSigner.Fn,
			erc20types.MsgConvertERC20CustomGetSigner.MsgType: erc20types.MsgConvertERC20CustomGetSigner.Fn,
		},
	}

	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     gogoproto.HybridResolver,
		SigningOptions: signingOptions,
	})
	if err != nil {
		return EncodingConfig{}, err
	}

	evmcodec.RegisterInterfaces(interfaceRegistry)

	appCodec := codec.NewProtoCodec(interfaceRegistry)
	txConfig, err := authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{
		EnabledSignModes: authtx.DefaultSignModes,
		SigningOptions:   &signingOptions,
		SigningContext:   interfaceRegistry.SigningContext(),
	})
	if err != nil {
		return EncodingConfig{}, err
	}

	legacyAmino := codec.NewLegacyAmino()
	evmcodec.RegisterLegacyAminoCodec(legacyAmino)
	// Publish process-wide EIP-712 compatibility state only after every
	// fallible encoding component has been built successfully.
	eip712.SetEncodingConfig(legacyAmino, interfaceRegistry, config.EVMChainID)
	legacytx.RegressionTestingAminoCodec = legacyAmino

	return EncodingConfig{
		LegacyAmino:       legacyAmino,
		InterfaceRegistry: interfaceRegistry,
		Codec:             appCodec,
		TxConfig:          txConfig,
	}, nil
}
