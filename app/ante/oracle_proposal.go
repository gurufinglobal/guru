package ante

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

// WrapAnteHandlerWithOracleProposalOptionBlock reserves the Oracle proposal
// extension for proposer-injected consensus records. It checks both extension
// lists because the SDK intentionally ignores unknown non-critical options.
func WrapAnteHandlerWithOracleProposalOptionBlock(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if extensionTx, ok := tx.(authante.HasExtensionOptionsTx); ok {
			for _, options := range [][]*codectypes.Any{
				extensionTx.GetExtensionOptions(),
				extensionTx.GetNonCriticalExtensionOptions(),
			} {
				for _, option := range options {
					if option != nil && option.GetTypeUrl() == oracletypes.ProposalPayloadTypeURL {
						return ctx, sdkerrors.ErrUnknownExtensionOptions.Wrap("Oracle proposal option is reserved for consensus records")
					}
				}
			}
		}

		return next(ctx, tx, simulate)
	}
}
