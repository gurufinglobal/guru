package keeper

import (
	"context"
	"fmt"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// Transfer intercepts PFM-initiated MsgTransfer forwarding, charges fee from the
// PFM-created sender (msg.Sender), and forwards the net amount using the original
// transfer keeper.
//
// Fee is computed as: floor(amount * fee_percentage).
func (k Keeper) Transfer(ctx context.Context, msg *transfertypes.MsgTransfer) (*transfertypes.MsgTransferResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil MsgTransfer")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, fmt.Errorf("invalid sender address %q: %w", msg.Sender, err)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	feePct := params.FeePercentage
	if feePct.IsNil() {
		return nil, fmt.Errorf("fee_percentage is nil")
	}
	if feePct.IsNegative() {
		return nil, fmt.Errorf("fee_percentage cannot be negative: %s", feePct)
	}
	if feePct.IsZero() {
		return k.originalKeeper.Transfer(ctx, msg)
	}

	amount := msg.Token.Amount
	if amount.IsNil() || !amount.IsPositive() {
		return nil, fmt.Errorf("invalid amount: %s", amount)
	}

	feeAmt := feePct.MulInt(amount).TruncateInt()
	if feeAmt.IsNegative() {
		return nil, fmt.Errorf("computed fee is negative: %s", feeAmt)
	}
	if feeAmt.IsZero() {
		return k.originalKeeper.Transfer(ctx, msg)
	}

	netAmt := amount.Sub(feeAmt)
	if !netAmt.IsPositive() {
		return nil, fmt.Errorf("net amount must be positive: amount=%s fee=%s", amount, feeAmt)
	}

	feeCoin := sdk.NewCoin(msg.Token.Denom, feeAmt)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.EscrowModuleName, sdk.NewCoins(feeCoin)); err != nil {
		return nil, fmt.Errorf("failed to lock fee into %q module account: %w", types.EscrowModuleName, err)
	}

	// update MsgTransfer to forward the net amount
	msg.Token.Amount = netAmt

	resp, err := k.originalKeeper.Transfer(ctx, msg)
	if err != nil {
		return nil, err
	}

	// Track the locked fee by the outgoing packet key (port/channel/sequence).
	if err := k.SetLockedFee(sdkCtx, msg.SourcePort, msg.SourceChannel, resp.Sequence, feeCoin); err != nil {
		return nil, err
	}

	return resp, nil
}

// --- delegate methods required by PFM's TransferKeeper interface ---

func (k Keeper) GetDenom(ctx sdk.Context, denomHash cmtbytes.HexBytes) (transfertypes.Denom, bool) {
	return k.originalKeeper.GetDenom(ctx, denomHash)
}

func (k Keeper) GetTotalEscrowForDenom(ctx sdk.Context, denom string) sdk.Coin {
	return k.originalKeeper.GetTotalEscrowForDenom(ctx, denom)
}

func (k Keeper) SetTotalEscrowForDenom(ctx sdk.Context, coin sdk.Coin) {
	k.originalKeeper.SetTotalEscrowForDenom(ctx, coin)
}

func (k Keeper) DenomPathFromHash(ctx sdk.Context, ibcDenom string) (string, error) {
	return k.originalKeeper.DenomPathFromHash(ctx, ibcDenom)
}

func (k Keeper) GetPort(ctx sdk.Context) string {
	return k.originalKeeper.GetPort(ctx)
}
