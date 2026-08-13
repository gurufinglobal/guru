package app

import (
	"sync"

	txsigning "cosmossdk.io/x/tx/signing"
	"cosmossdk.io/x/tx/signing/textual"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	evmaddress "github.com/cosmos/evm/encoding/address"
	evmcodec "github.com/cosmos/evm/encoding/codec"
	"github.com/cosmos/evm/ethereum/eip712"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
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

// ConfigureEIP712ChainID publishes the selected network's EIP-712 domain to
// Cosmos EVM's process-wide signing compatibility layer. A gurud process runs
// one network, while the same binary may be configured with different values
// in separate processes. Callers must invoke this only during single-threaded
// startup, before signing or transaction handling begins.
func ConfigureEIP712ChainID(chainID uint64) error {
	encoding, err := MakeEncodingConfig()
	if err != nil {
		return err
	}

	eip712.SetEncodingConfig(encoding.LegacyAmino, encoding.InterfaceRegistry, chainID)
	return nil
}

// NewTextualTxConfig creates a transaction configuration with
// SIGN_MODE_TEXTUAL. Validators supply a deterministic BankKeeper-backed
// metadata query, while online clients supply a gRPC/ABCI-backed query.
func NewTextualTxConfig(metadataQuery textual.CoinMetadataQueryFn) (client.TxConfig, error) {
	encoding, err := MakeEncodingConfig()
	if err != nil {
		return nil, err
	}

	enabledSignModes := append([]signingtypes.SignMode(nil), authtx.DefaultSignModes...)
	enabledSignModes = append(enabledSignModes, signingtypes.SignMode_SIGN_MODE_TEXTUAL)
	return newTxConfig(
		encoding.Codec,
		encoding.InterfaceRegistry,
		enabledSignModes,
		metadataQuery,
	)
}

func newSigningOptions() txsigning.Options {
	return txsigning.Options{
		FileResolver:          gogoproto.HybridResolver,
		AddressCodec:          evmaddress.NewEvmCodec(config.Bech32PrefixAccAddr),
		ValidatorAddressCodec: evmaddress.NewEvmCodec(config.Bech32PrefixValAddr),
		CustomGetSigners: map[protoreflect.FullName]txsigning.GetSignersFunc{
			evmtypes.MsgEthereumTxCustomGetSigner.MsgType:     evmtypes.MsgEthereumTxCustomGetSigner.Fn,
			erc20types.MsgConvertERC20CustomGetSigner.MsgType: erc20types.MsgConvertERC20CustomGetSigner.Fn,
		},
	}
}

func newTxConfig(
	appCodec codec.Codec,
	interfaceRegistry codectypes.InterfaceRegistry,
	enabledSignModes []signingtypes.SignMode,
	metadataQuery textual.CoinMetadataQueryFn,
) (client.TxConfig, error) {
	signingOptions := newSigningOptions()
	return authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{
		EnabledSignModes:           enabledSignModes,
		SigningOptions:             &signingOptions,
		SigningContext:             interfaceRegistry.SigningContext(),
		TextualCoinMetadataQueryFn: metadataQuery,
	})
}

func buildEncodingConfig() (EncodingConfig, error) {
	signingOptions := newSigningOptions()

	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     gogoproto.HybridResolver,
		SigningOptions: signingOptions,
	})
	if err != nil {
		return EncodingConfig{}, err
	}

	evmcodec.RegisterInterfaces(interfaceRegistry)
	constitutiontypes.RegisterInterfaces(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	evmcodec.RegisterLegacyAminoCodec(legacyAmino)
	moduleBasics := NewBasicManager()
	moduleBasics.RegisterInterfaces(interfaceRegistry)
	moduleBasics.RegisterLegacyAminoCodec(legacyAmino)

	appCodec := codec.NewProtoCodec(interfaceRegistry)
	txConfig, err := newTxConfig(appCodec, interfaceRegistry, authtx.DefaultSignModes, nil)
	if err != nil {
		return EncodingConfig{}, err
	}

	// Publish process-wide EIP-712 compatibility state only after every
	// fallible encoding component has been built successfully.
	eip712.SetEncodingConfig(legacyAmino, interfaceRegistry, config.DefaultEVMChainID)
	legacytx.RegressionTestingAminoCodec = legacyAmino

	return EncodingConfig{
		LegacyAmino:       legacyAmino,
		InterfaceRegistry: interfaceRegistry,
		Codec:             appCodec,
		TxConfig:          txConfig,
	}, nil
}
