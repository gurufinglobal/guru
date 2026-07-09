package params

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
)

type pulsarTxConfig struct {
	client.TxConfig

	cdc     codec.Codec
	decoder sdk.TxDecoder
	encoder sdk.TxEncoder
}

func newPulsarTxConfig(base client.TxConfig, cdc codec.Codec) (client.TxConfig, error) {
	decoder, err := newPulsarTxDecoder(cdc)
	if err != nil {
		return nil, err
	}

	return pulsarTxConfig{
		TxConfig: base,
		cdc:      cdc,
		decoder:  decoder,
		encoder:  newPulsarTxEncoder(base.TxEncoder()),
	}, nil
}

func (c pulsarTxConfig) TxDecoder() sdk.TxDecoder {
	return c.decoder
}

func (c pulsarTxConfig) TxEncoder() sdk.TxEncoder {
	return c.encoder
}

func (c pulsarTxConfig) WrapTxBuilder(tx sdk.Tx) (client.TxBuilder, error) {
	decoded, ok := tx.(*pulsarDecodedTx)
	if !ok {
		return c.TxConfig.WrapTxBuilder(tx)
	}

	builder := c.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(decoded.GetMsgs()...); err != nil {
		return nil, err
	}

	builder.SetMemo(decoded.GetMemo())
	builder.SetTimeoutHeight(decoded.GetTimeoutHeight())
	builder.SetTimeoutTimestamp(decoded.GetTimeoutTimeStamp())
	builder.SetUnordered(decoded.GetUnordered())
	builder.SetGasLimit(decoded.GetGas())
	builder.SetFeeAmount(decoded.GetFee())

	if fee := decoded.tx.AuthInfo.Fee; fee != nil {
		if fee.Payer != "" {
			addr, err := c.cdc.InterfaceRegistry().SigningContext().AddressCodec().StringToBytes(fee.Payer)
			if err != nil {
				return nil, err
			}
			builder.SetFeePayer(addr)
		}
		if fee.Granter != "" {
			addr, err := c.cdc.InterfaceRegistry().SigningContext().AddressCodec().StringToBytes(fee.Granter)
			if err != nil {
				return nil, err
			}
			builder.SetFeeGranter(addr)
		}
	}

	if extBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder); ok {
		extBuilder.SetExtensionOptions(decoded.GetExtensionOptions()...)
		extBuilder.SetNonCriticalExtensionOptions(decoded.GetNonCriticalExtensionOptions()...)
	}

	sigs, err := decoded.GetSignaturesV2()
	if err != nil {
		return nil, err
	}
	if len(sigs) > 0 {
		if err := builder.SetSignatures(sigs...); err != nil {
			return nil, err
		}
	}

	return builder, nil
}
