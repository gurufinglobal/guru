package encoding

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	amino "github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"

	"github.com/cosmos/gogoproto/proto"

	cryptocodec "github.com/cosmos/evm/crypto/codec"
	evmaddress "github.com/cosmos/evm/encoding/address"
	"github.com/cosmos/evm/ethereum/eip712"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

type Config struct {
	InterfaceRegistry codectypes.InterfaceRegistry
	Codec             amino.Codec
	TxConfig          client.TxConfig
	Amino             *amino.LegacyAmino
}

func MakeConfig(evmChainID uint64, accountPrefix string) Config {
	aminoCdc := codec.NewLegacyAmino()

	signingOptions := signing.Options{
		AddressCodec:          evmaddress.NewEvmCodec(accountPrefix),
		ValidatorAddressCodec: evmaddress.NewEvmCodec(accountPrefix + "valoper"),

		CustomGetSigners: map[protoreflect.FullName]signing.GetSignersFunc{
			evmtypes.MsgEthereumTxCustomGetSigner.MsgType:     evmtypes.MsgEthereumTxCustomGetSigner.Fn,
			erc20types.MsgConvertERC20CustomGetSigner.MsgType: erc20types.MsgConvertERC20CustomGetSigner.Fn,
		},
	}

	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     proto.HybridResolver,
		SigningOptions: signingOptions,
	})
	if err != nil {
		panic(err)
	}

	std.RegisterInterfaces(interfaceRegistry)
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	eip712.RegisterInterfaces(interfaceRegistry)

	std.RegisterLegacyAminoCodec(aminoCdc)
	cryptocodec.RegisterCrypto(aminoCdc)
	codec.RegisterEvidences(aminoCdc)

	appCodec := codec.NewProtoCodec(interfaceRegistry)

	eip712.SetEncodingConfig(aminoCdc, interfaceRegistry, evmChainID)

	legacytx.RegressionTestingAminoCodec = aminoCdc

	txConfig := tx.NewTxConfig(appCodec, tx.DefaultSignModes)

	return Config{
		InterfaceRegistry: interfaceRegistry,
		Codec:             appCodec,
		TxConfig:          txConfig,
		Amino:             aminoCdc,
	}
}
