package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/gurufinglobal/guru/v3/x/feepolicy/types"
)

var _ types.MsgServer = (*MsgServer)(nil)

// MsgServer implements the feepolicy transaction service.
type MsgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) *MsgServer {
	if keeper == nil {
		panic("feepolicy msg server keeper cannot be nil")
	}
	return &MsgServer{keeper: keeper}
}

func (s *MsgServer) ChangeModerator(
	goCtx context.Context,
	msg *types.MsgChangeModerator,
) (*types.MsgChangeModeratorResponse, error) {
	if msg == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("request cannot be nil")
	}

	oldModerator, err := s.keeper.authorizeModerator(goCtx, msg.ModeratorAddress)
	if err != nil {
		return nil, err
	}
	newModerator, _, err := s.keeper.canonicalModeratorAddress(msg.NewModeratorAddress)
	if err != nil {
		return nil, err
	}
	if err := s.keeper.constitutionKeeper.UpdateModeratorAddress(goCtx, newModerator); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeChangeModerator,
		sdk.NewAttribute(types.AttributeKeyModerator, oldModerator),
		sdk.NewAttribute(types.AttributeKeyAddress, newModerator),
	))

	return &types.MsgChangeModeratorResponse{}, nil
}

func (s *MsgServer) RegisterDiscounts(
	goCtx context.Context,
	msg *types.MsgRegisterDiscounts,
) (*types.MsgRegisterDiscountsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("request cannot be nil")
	}
	moderator, err := s.keeper.authorizeModerator(goCtx, msg.ModeratorAddress)
	if err != nil {
		return nil, err
	}
	if len(msg.Discounts) == 0 {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("no discounts provided")
	}
	// Normalize the complete batch before the first write so direct service
	// calls cannot leave a partially registered batch on validation failure.
	normalized := make([]types.AccountDiscount, len(msg.Discounts))
	accounts := make(map[string]struct{}, len(msg.Discounts))
	for i, discount := range msg.Discounts {
		normalized[i], err = s.keeper.NormalizeAccountDiscount(discount)
		if err != nil {
			return nil, err
		}
		if _, exists := accounts[normalized[i].Address]; exists {
			return nil, sdkerrors.ErrInvalidRequest.Wrapf("duplicate discount address %q", normalized[i].Address)
		}
		accounts[normalized[i].Address] = struct{}{}
	}
	for _, discount := range normalized {
		if err := s.keeper.SetAccountDiscounts(goCtx, discount); err != nil {
			return nil, err
		}
	}

	attributes := []sdk.Attribute{sdk.NewAttribute(types.AttributeKeyModerator, moderator)}
	for _, accountDiscount := range normalized {
		attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyAddress, accountDiscount.Address))
		for _, moduleDiscount := range accountDiscount.Modules {
			attributes = append(attributes, sdk.NewAttribute(types.AttributeKeyModule, moduleDiscount.Module))
			for _, discount := range moduleDiscount.Discounts {
				attributes = append(attributes,
					sdk.NewAttribute(types.AttributeKeyMsgType, discount.MsgType),
					sdk.NewAttribute(types.AttributeKeyDiscountType, discount.DiscountType),
					sdk.NewAttribute(types.AttributeKeyAmount, discount.Amount.String()),
				)
			}
		}
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeRegisterDiscounts, attributes...))
	return &types.MsgRegisterDiscountsResponse{}, nil
}

func (s *MsgServer) RemoveDiscounts(
	goCtx context.Context,
	msg *types.MsgRemoveDiscounts,
) (*types.MsgRemoveDiscountsResponse, error) {
	if msg == nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("request cannot be nil")
	}
	moderator, err := s.keeper.authorizeModerator(goCtx, msg.ModeratorAddress)
	if err != nil {
		return nil, err
	}

	address := msg.Address
	if address != "" {
		address, _, err = s.keeper.canonicalAddress(address)
		if err != nil {
			return nil, err
		}
	}
	switch {
	case msg.Module == "":
		err = s.keeper.DeleteAccountDiscounts(goCtx, address)
	case msg.MsgType == "":
		err = s.keeper.DeleteModuleDiscounts(goCtx, address, msg.Module)
	default:
		// Compatibility: v2 ignored Module in this branch and removed the
		// first matching message type from every module.
		err = s.keeper.DeleteMsgTypeDiscounts(goCtx, address, msg.MsgType)
	}
	if err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRemoveDiscounts,
		sdk.NewAttribute(types.AttributeKeyModerator, moderator),
		sdk.NewAttribute(types.AttributeKeyAddress, address),
		sdk.NewAttribute(types.AttributeKeyModule, msg.Module),
		sdk.NewAttribute(types.AttributeKeyMsgType, msg.MsgType),
	))

	return &types.MsgRemoveDiscountsResponse{}, nil
}
