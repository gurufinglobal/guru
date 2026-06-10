package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
)

func WrapAnteHandlerWithLegacyGovBlock(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if err := rejectLegacyGovMessages(tx); err != nil {
			return ctx, err
		}

		return next(ctx, tx, simulate)
	}
}

func rejectLegacyGovMessages(tx sdk.Tx) error {
	if tx == nil {
		return nil
	}

	for _, msg := range tx.GetMsgs() {
		if err := rejectLegacyGovMsg(msg); err != nil {
			return err
		}
	}

	return nil
}

func rejectLegacyGovMsg(msg sdk.Msg) error {
	switch m := msg.(type) {
	case *govv1.MsgExecLegacyContent, *govv1beta1.MsgSubmitProposal:
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "legacy gov proposal messages are not supported")
	case *authztypes.MsgExec:
		msgs, err := m.GetMessages()
		if err != nil {
			return err
		}
		for _, nested := range msgs {
			if err := rejectLegacyGovMsg(nested); err != nil {
				return err
			}
		}
	}

	return nil
}
