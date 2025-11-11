package keeper

import (
	"context"
	"fmt"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/gurufinglobal/guru/v2/x/bex/types"
)

// MsgServer implementation
var _ types.MsgServer = &Keeper{}

// RegisterAdmin implements types.MsgServer.
func (k Keeper) RegisterAdmin(goCtx context.Context, msg *types.MsgRegisterAdmin) (*types.MsgRegisterAdminResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator_address := k.GetModeratorAddress(ctx)
	if moderator_address != msg.ModeratorAddress {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator_address, msg.ModeratorAddress)
	}

	_, err := sdk.AccAddressFromBech32(msg.AdminAddress)
	if err != nil {
		return nil, err
	}

	k.AddAdmin(ctx, msg.AdminAddress)
	exchangeId := ""
	if !msg.ExchangeId.IsNil() && !msg.ExchangeId.IsZero() {
		exchangeId = msg.ExchangeId.String()
		exchange, err := k.GetExchange(ctx, msg.ExchangeId)
		if err != nil {
			return nil, err
		}
		exchange.AdminAddress = msg.AdminAddress
		err = k.SetExchange(ctx, exchange)
		if err != nil {
			return nil, err
		}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRegisterAdmin,
			sdk.NewAttribute(types.AttributeKeyModerator, moderator_address),
			sdk.NewAttribute(types.AttributeKeyAddress, msg.AdminAddress),
			sdk.NewAttribute(types.AttributeKeyExchangeId, exchangeId),
		),
	)

	return &types.MsgRegisterAdminResponse{}, nil
}

// RemoveAdmin implements types.MsgServer.
func (k Keeper) RemoveAdmin(goCtx context.Context, msg *types.MsgRemoveAdmin) (*types.MsgRemoveAdminResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator_address := k.GetModeratorAddress(ctx)
	if moderator_address != msg.ModeratorAddress {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator_address, msg.ModeratorAddress)
	}

	_, err := sdk.AccAddressFromBech32(msg.AdminAddress)
	if err != nil {
		return nil, err
	}

	k.DeleteAdmin(ctx, msg.AdminAddress)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRemoveAdmin,
			sdk.NewAttribute(types.AttributeKeyModerator, moderator_address),
			sdk.NewAttribute(types.AttributeKeyAddress, msg.AdminAddress),
		),
	)

	return &types.MsgRemoveAdminResponse{}, nil
}

// RegisterExchange implements types.MsgServer.
func (k Keeper) RegisterExchange(goCtx context.Context, msg *types.MsgRegisterExchange) (*types.MsgRegisterExchangeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	err := msg.Exchange.Validate()
	if err != nil {
		return nil, err
	}

	// validate the admin address
	if !k.IsAdminRegistered(ctx, msg.AdminAddress) {
		return nil, errorsmod.Wrapf(types.ErrWrongAdmin, "%s is not an admin", msg.AdminAddress)
	}
	if msg.AdminAddress != msg.Exchange.AdminAddress {
		return nil, errorsmod.Wrapf(types.ErrWrongAdmin, " only sender can be admin of the exchange")
	}

	// validate the ID
	nextExchangeId, err := k.GetNextExchangeId(ctx)
	if err != nil {
		return nil, err
	}
	if !msg.Exchange.Id.Equal(nextExchangeId) {
		return nil, errorsmod.Wrapf(types.ErrInvalidId, " expected: %s, got: %s", nextExchangeId, msg.Exchange.Id)
	}

	err = k.SetExchange(ctx, msg.Exchange)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRegisterExchange,
			sdk.NewAttribute(types.AttributeKeyAdmin, msg.AdminAddress),
			sdk.NewAttribute(types.AttributeKeyExchangeId, msg.Exchange.Id.String()),
		),
	)

	return &types.MsgRegisterExchangeResponse{}, nil
}

// UpdateExchange implements types.MsgServer.
func (k Keeper) UpdateExchange(goCtx context.Context, msg *types.MsgUpdateExchange) (*types.MsgUpdateExchangeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	exchange, err := k.GetExchange(ctx, msg.ExchangeId)
	if err != nil {
		return nil, err
	}

	// validate the admin address
	if msg.AdminAddress != exchange.AdminAddress {
		return nil, errorsmod.Wrapf(types.ErrWrongAdmin, " expected: %s, got: %s", exchange.AdminAddress, msg.AdminAddress)
	}

	// key is admin address
	if msg.Key == types.ExchangeKeyAdminAddress {
		_, err := sdk.AccAddressFromBech32(msg.Value)
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin address: %s", err)
		}
		exchange.AdminAddress = msg.Value

		// key is reserve address
	} else if msg.Key == types.ExchangeKeyReserveAddress {
		_, err := sdk.AccAddressFromBech32(msg.Value)
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " reserve address: %s", err)
		}
		exchange.ReserveAddress = msg.Value

		// key is fee
	} else if msg.Key == types.ExchangeKeyFee {
		feeDec, err := math.LegacyNewDecFromStr(msg.Value)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidFee, " %s", err)
		}
		if err := types.ValidateExchangeFee(feeDec); err != nil {
			return nil, err
		}
		exchange.Fee = feeDec

		// key is limit
	} else if msg.Key == types.ExchangeKeyLimit {
		limitDec, err := math.LegacyNewDecFromStr(msg.Value)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidLimit, " %s", err)
		}
		if err := types.ValidateExchangeLimit(limitDec); err != nil {
			return nil, err
		}
		exchange.Limit = limitDec

		// key is status
	} else if msg.Key == types.ExchangeKeyStatus {
		if err := types.ValidateExchangeStatus(msg.Value); err != nil {
			return nil, err
		}
		exchange.Status = msg.Value

		// key is metadata
	} else if msg.Key == types.ExchangeKeyMetadata {
		metadataMap, err := types.ValidateAndUnmarshalExchangeMetedataFromStr(msg.Value)
		if err != nil {
			return nil, err
		}
		for key, value := range metadataMap {
			exchange.Metadata[key] = value
		}
	}

	err = k.SetExchange(ctx, exchange)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUpdateExchange,
			sdk.NewAttribute(types.AttributeKeyAdmin, msg.AdminAddress),
			sdk.NewAttribute(types.AttributeKeyExchangeId, msg.ExchangeId.String()),
			sdk.NewAttribute(types.AttributeKeyKey, msg.Key),
			sdk.NewAttribute(types.AttributeKeyValue, msg.Value),
		),
	)

	return &types.MsgUpdateExchangeResponse{}, nil
}

// UpdateRatemeter implements types.MsgServer.
func (k Keeper) UpdateRatemeter(goCtx context.Context, msg *types.MsgUpdateRatemeter) (*types.MsgUpdateRatemeterResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	moderatorAddress := k.GetModeratorAddress(ctx)
	if msg.ModeratorAddress != moderatorAddress {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderatorAddress, msg.ModeratorAddress)
	}

	ratemeter, err := k.GetRatemeter(ctx)
	if err != nil {
		return nil, err
	}

	if msg.Ratemeter.RequestCountLimit > 0 {
		ratemeter.RequestCountLimit = msg.Ratemeter.RequestCountLimit
	}
	if msg.Ratemeter.RequestPeriod > 0 {
		ratemeter.RequestPeriod = msg.Ratemeter.RequestPeriod
	}

	k.SetRatemeter(ctx, ratemeter)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUpdateRatemeter,
			sdk.NewAttribute(types.AttributeKeyRequestCountLimit, strconv.FormatUint(ratemeter.RequestCountLimit, 10)),
			sdk.NewAttribute(types.AttributeKeyRequestPeriod, ratemeter.RequestPeriod.String()),
		),
	)

	return &types.MsgUpdateRatemeterResponse{}, nil
}

// WithdrawFees implements types.MsgServer.
func (k Keeper) WithdrawFees(goCtx context.Context, msg *types.MsgWithdrawFees) (*types.MsgWithdrawFeesResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)

	// exchange, err := k.GetExchange(ctx, msg.ExchangeId)
	// if err != nil {
	// 	return nil, err
	// }

	// // validate the admin address
	// if msg.AdminAddress != exchange.AdminAddress {
	// 	return nil, errorsmod.Wrapf(types.ErrWrongAdmin, " expected: %s, got: %s", exchange.AdminAddress, msg.AdminAddress)
	// }

	// err = k.WithdrawExchangeFees(ctx, msg.ExchangeId.String(), msg.WithdrawAddress)
	// if err != nil {
	// 	return nil, err
	// }

	// ctx.EventManager().EmitEvent(
	// 	sdk.NewEvent(
	// 		types.EventTypeWithdrawFees,
	// 		sdk.NewAttribute(types.AttributeKeyAdmin, msg.AdminAddress),
	// 		sdk.NewAttribute(types.AttributeKeyExchangeId, msg.ExchangeId.String()),
	// 		sdk.NewAttribute(types.AttributeKeyWithdrawAddress, msg.WithdrawAddress),
	// 		sdk.NewAttribute(types.AttributeKeyAmount, exchange.AccumulatedFee.String()),
	// 	),
	// )

	return nil, fmt.Errorf("not implemented")
}

// ChangeModerator implements types.MsgServer.
func (k Keeper) ChangeModerator(goCtx context.Context, msg *types.MsgChangeBexModerator) (*types.MsgChangeBexModeratorResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	moderator_address := k.GetModeratorAddress(ctx)
	if msg.ModeratorAddress != moderator_address {
		return nil, errorsmod.Wrapf(types.ErrWrongModerator, ", expected: %s, got: %s", moderator_address, msg.ModeratorAddress)
	}

	_, err := sdk.AccAddressFromBech32(msg.NewModeratorAddress)
	if err != nil {
		return nil, err
	}

	// Update the KV store
	k.SetModeratorAddress(ctx, msg.NewModeratorAddress)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeChangeModerator,
			sdk.NewAttribute(types.AttributeKeyModerator, msg.ModeratorAddress),
			sdk.NewAttribute(types.AttributeKeyAddress, msg.NewModeratorAddress),
		),
	)

	return &types.MsgChangeBexModeratorResponse{}, nil
}
