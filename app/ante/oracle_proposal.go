package ante

import (
	storetypes "cosmossdk.io/store/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
)

// WrapAnteHandlerWithOracleProposalOptionBlock reserves the Oracle proposal
// extension for proposer-injected consensus records. It checks both extension
// lists because the SDK intentionally ignores unknown non-critical options.
func WrapAnteHandlerWithOracleProposalOptionBlock(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if extensionTx, ok := tx.(authante.HasExtensionOptionsTx); ok {
			critical := extensionTx.GetExtensionOptions()
			nonCritical := extensionTx.GetNonCriticalExtensionOptions()

			for _, option := range nonCritical {
				if isOracleProposalOption(option) {
					return rejectOracleProposalOption(ctx)
				}
			}

			for _, option := range critical {
				if !isOracleProposalOption(option) {
					continue
				}

				// Cosmos SDK v0.53 has no TxRunner hook that can remove the
				// proposer-injected record before DeliverTx. ProcessProposal has
				// already verified its canonical bytes and Txs[0] position, so
				// finalize it as a zero-gas, zero-message system transaction.
				if ctx.ExecMode() == sdk.ExecModeFinalize &&
					len(critical) == 1 && len(nonCritical) == 0 && len(tx.GetMsgs()) == 0 {
					return ctx.WithGasMeter(storetypes.NewGasMeter(0)), nil
				}

				return rejectOracleProposalOption(ctx)
			}
		}

		return next(ctx, tx, simulate)
	}
}

func isOracleProposalOption(option *codectypes.Any) bool {
	return option != nil && option.GetTypeUrl() == oracletypes.ProposalPayloadTypeURL
}

func rejectOracleProposalOption(ctx sdk.Context) (sdk.Context, error) {
	return ctx, sdkerrors.ErrUnknownExtensionOptions.Wrap("Oracle proposal option is reserved for consensus records")
}
