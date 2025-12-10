package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v2/x/feepolicy/types"
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

	// Emit event for changing moderator address
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeChangeModerator,
			sdk.NewAttribute(types.AttributeKeyModerator, msg.ModeratorAddress),
			sdk.NewAttribute(types.AttributeKeyAddress, msg.NewModeratorAddress),
		),
	)

	return &types.MsgChangeModeratorResponse{}, nil
}

// RegisterDiscounts implements types.MsgServer.
func (k Keeper) RegisterDiscounts(goCtx context.Context, msg *types.MsgRegisterDiscounts) (*types.MsgRegisterDiscountsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	if msg.ModeratorAddress != moderator.Address {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator.Address, msg.ModeratorAddress)
	}

	// Set the discounts for the account
	for _, discount := range msg.Discounts {
		err := types.ValidateAccountDiscount(discount)
		if err != nil {
			return nil, err
		}
		k.SetAccountDiscounts(ctx, discount)
	}

	// manually create the event attributes for all discounts
	var attributes []sdk.Attribute
	attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyModerator, msg.ModeratorAddress))
	for _, accDiscount := range msg.Discounts {
		attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyAddress, accDiscount.Address))
		for _, moduleDiscount := range accDiscount.Modules {
			attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyModule, moduleDiscount.Module))
			for _, discount := range moduleDiscount.Discounts {
				attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyMsgType, discount.MsgType))
				attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyDiscountType, discount.DiscountType))
				attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyAmount, discount.Amount.String()))
			}
		}
	}

	// Emit event for registering discounts
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRegisterDiscounts,
			attributes...,
		),
	)
	return &types.MsgRegisterDiscountsResponse{}, nil
}

// RemoveDiscounts implements types.MsgServer.
func (k Keeper) RemoveDiscounts(goCtx context.Context, msg *types.MsgRemoveDiscounts) (*types.MsgRemoveDiscountsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	if msg.ModeratorAddress != moderator.Address {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator.Address, msg.ModeratorAddress)
	}

	// update the KV store
	switch {
	case msg.Module == "":
		k.DeleteAccountDiscounts(ctx, msg.Address)
	case msg.MsgType == "":
		k.DeleteModuleDiscounts(ctx, msg.Address, msg.Module)
	default:
		k.DeleteMsgTypeDiscounts(ctx, msg.Address, msg.MsgType)
	}

	// Emit event for removing discounts
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRemoveDiscounts,
			sdk.NewAttribute(types.AttributeKeyModerator, msg.ModeratorAddress),
			sdk.NewAttribute(types.AttributeKeyAddress, msg.Address),
			sdk.NewAttribute(types.AttributeKeyModule, msg.Module),
			sdk.NewAttribute(types.AttributeKeyMsgType, msg.MsgType),
		),
	)

	return &types.MsgRemoveDiscountsResponse{}, nil
}
