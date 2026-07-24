package ante

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	evmante "github.com/cosmos/evm/ante"
	antetypes "github.com/cosmos/evm/ante/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"

	feepolicytypes "github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

// FeePolicyKeeper is the fee policy functionality required by the Cosmos ante
// handler. State and decoding errors must be returned so fee deduction fails
// closed.
type FeePolicyKeeper interface {
	ResolveDiscount(ctx context.Context, feePayerAddr string, msgs []sdk.Msg) (feepolicytypes.Discount, error)
}

// HandlerOptions combines the complete upstream Cosmos EVM options with the
// one dependency used only by Guru's Cosmos transaction path.
type HandlerOptions struct {
	EVMOptions      evmante.HandlerOptions
	FeePolicyKeeper FeePolicyKeeper
}

// Validate checks all dependencies used by both routed ante handlers.
func (options HandlerOptions) Validate() error {
	if err := options.EVMOptions.Validate(); err != nil {
		return err
	}
	if options.FeePolicyKeeper == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "fee policy keeper is required for AnteHandler")
	}

	return nil
}

// NewAnteHandler routes Ethereum extension transactions to the unmodified
// upstream Cosmos EVM handler and routes normal and dynamic-fee Cosmos
// transactions to Guru's fee-policy-aware Cosmos chain.
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	upstreamHandler := evmante.NewAnteHandler(options.EVMOptions)
	ethereumExtension := sdk.MsgTypeURL(&evmtypes.ExtensionOptionsEthereumTx{})
	dynamicFeeExtension := sdk.MsgTypeURL(&antetypes.ExtensionOptionDynamicFeeTx{})

	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if txWithExtensions, ok := tx.(authante.HasExtensionOptionsTx); ok {
			extensions := txWithExtensions.GetExtensionOptions()
			if len(extensions) > 0 {
				switch extensions[0].GetTypeUrl() {
				case ethereumExtension:
					// Do not copy or wrap the Ethereum mono ante chain. Delegating
					// here preserves EVM fee deduction, virtual fee collection and
					// pending transaction listener behavior from Cosmos EVM v0.7.
					return upstreamHandler(ctx, tx, simulate)
				case dynamicFeeExtension:
					return newCosmosAnteHandler(ctx, options)(ctx, tx, simulate)
				default:
					// Let upstream produce the canonical unsupported-extension
					// error and preserve its routing semantics.
					return upstreamHandler(ctx, tx, simulate)
				}
			}
		}

		if tx == nil {
			return upstreamHandler(ctx, tx, simulate)
		}

		return newCosmosAnteHandler(ctx, options)(ctx, tx, simulate)
	}, nil
}
