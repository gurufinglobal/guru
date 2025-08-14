package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/GPTx-global/guru-v2/x/feepolicy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MsgServer implementation
var _ types.MsgServer = &Keeper{}

// ChangeModerator implements types.MsgServer.
func (k Keeper) ChangeModerator(goCtx context.Context, msg *types.MsgChangeModerator) (*types.MsgChangeModeratorResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	if msg.ModeratorAddress != moderator.Address {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator.Address, msg.ModeratorAddress)
	}

	_, err = sdk.AccAddressFromBech32(msg.NewModeratorAddress)
	if err != nil {
		return nil, err
	}

	// Update the KV store
	k.SetModeratorAddress(ctx, types.Moderator{Address: msg.NewModeratorAddress})

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeChangeModerator,
			sdk.NewAttribute(types.AttributeKeyModerator, msg.ModeratorAddress),
			sdk.NewAttribute(types.AttributeKeyAddress, msg.NewModeratorAddress),
		),
	)

	return &types.MsgChangeModeratorResponse{}, nil
}

func (k Keeper) RegisterDiscounts(goCtx context.Context, msg *types.MsgRegisterDiscounts) (*types.MsgRegisterDiscountsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	if msg.ModeratorAddress != moderator.Address {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator.Address, msg.ModeratorAddress)
	}

	for _, discount := range msg.Discounts {
		k.SetAccountDiscounts(ctx, discount)
	}

	return &types.MsgRegisterDiscountsResponse{}, nil
}

func (k Keeper) RemoveDiscounts(goCtx context.Context, msg *types.MsgRemoveDiscounts) (*types.MsgRemoveDiscountsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	if msg.ModeratorAddress != moderator.Address {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator.Address, msg.ModeratorAddress)
	}

	if msg.Module == "" {
		k.DeleteAccountDiscounts(ctx, msg.Address)
	} else if msg.MsgType == "" {
		k.DeleteModuleDiscounts(ctx, msg.Address, msg.Module)
	} else {
		k.DeleteMsgTypeDiscounts(ctx, msg.Address, msg.MsgType)
	}

	return &types.MsgRemoveDiscountsResponse{}, nil
}
