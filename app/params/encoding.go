package params

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"
	cryptocodec "github.com/cosmos/evm/crypto/codec"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/cosmos/evm/ethereum/eip712"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type EncodingConfig struct {
	InterfaceRegistry codectypes.InterfaceRegistry
	Codec             codec.Codec
	TxConfig          client.TxConfig
}

func MakeEncodingConfig(accountPrefix, validatorPrefix, consensusPrefix string) EncodingConfig {
	// SDK v0.54.3 txsigning.Options has no ConsensusAddressCodec field.
	// Keep the consensus prefix in the signature for forward compatibility
	// and to make all address prefixes explicit at call sites.
	_ = evmaddress.NewEvmCodec(consensusPrefix)

	signingOptions := &txsigning.Options{
		AddressCodec:          evmaddress.NewEvmCodec(accountPrefix),
		ValidatorAddressCodec: evmaddress.NewEvmCodec(validatorPrefix),
		CustomGetSigners:      customGetSigners(),
	}

	// HybridResolver is the pinned SDK/EVM boundary that resolves both SDK Pulsar descriptors and Guru's internal gogo descriptors.
	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     gogoproto.HybridResolver,
		SigningOptions: *signingOptions,
	})
	if err != nil {
		panic(err)
	}

	std.RegisterInterfaces(interfaceRegistry)
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	eip712.RegisterInterfaces(interfaceRegistry)

	appCodec := codec.NewProtoCodec(interfaceRegistry)

	signingOptions.FileResolver = interfaceRegistry

	baseTxConfig, err := authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{
		EnabledSignModes: []signingtypes.SignMode{
			signingtypes.SignMode_SIGN_MODE_DIRECT,
			signingtypes.SignMode_SIGN_MODE_DIRECT_AUX,
		},
		SigningOptions: signingOptions,
	})
	if err != nil {
		panic(err)
	}
	return EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Codec:             appCodec,
		TxConfig:          baseTxConfig,
	}
}

func customGetSigners() map[protoreflect.FullName]txsigning.GetSignersFunc {
	return map[protoreflect.FullName]txsigning.GetSignersFunc{
		evmtypes.MsgEthereumTxCustomGetSigner.MsgType:     evmtypes.MsgEthereumTxCustomGetSigner.Fn,
		erc20types.MsgConvertERC20CustomGetSigner.MsgType: erc20types.MsgConvertERC20CustomGetSigner.Fn,
	}
}
