package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

var _ types.MsgServer = (*Keeper)(nil)

func (k Keeper) RegisterAdmin(goCtx context.Context, req *types.MsgRegisterAdmin) (*types.MsgRegisterAdminResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	moderatorAddr, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}

	// Only the current moderator can register/replace the admin.
	if moderatorAddr != req.ModeratorAddress {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid moderator; expected %s, got %s", moderatorAddr, req.ModeratorAddress)
	}

	if _, err := sdk.AccAddressFromBech32(req.AdminAddress); err != nil {
		return nil, fmt.Errorf("invalid admin_address: %w", err)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	params.AdminAddress = req.AdminAddress
	if params.ReserveAddress == "" {
		params.ReserveAddress = req.AdminAddress
	}

	if err := params.Validate(); err != nil {
		return nil, err
	}
	if err := k.SetParams(ctx, params); err != nil {
		return nil, err
	}

	return &types.MsgRegisterAdminResponse{}, nil
}

func (k Keeper) UpdateFeePercentage(goCtx context.Context, req *types.MsgUpdateFeePercentage) (*types.MsgUpdateFeePercentageResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// Only the current admin can update fee_percentage.
	if params.AdminAddress != req.AdminAddress {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid admin; expected %s, got %s", params.AdminAddress, req.AdminAddress)
	}

	feePct := req.FeePercentage
	if feePct.IsNil() {
		return nil, fmt.Errorf("invalid fee_percentage: nil")
	}
	if feePct.IsNegative() {
		return nil, fmt.Errorf("invalid fee_percentage: cannot be negative: %s", feePct)
	}
	if feePct.GT(sdkmath.LegacyOneDec()) {
		return nil, fmt.Errorf("invalid fee_percentage: cannot be greater than 1: %s", feePct)
	}

	params.FeePercentage = feePct
	if err := k.SetParams(ctx, params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateFeePercentageResponse{}, nil
}

func (k Keeper) UpdateReserveAddress(goCtx context.Context, req *types.MsgUpdateReserveAddress) (*types.MsgUpdateReserveAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// Only the current admin can update reserve_address.
	if params.AdminAddress != req.AdminAddress {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid admin; expected %s, got %s", params.AdminAddress, req.AdminAddress)
	}

	if _, err := sdk.AccAddressFromBech32(req.ReserveAddress); err != nil {
		return nil, fmt.Errorf("invalid reserve_address: %w", err)
	}

	params.ReserveAddress = req.ReserveAddress
	if err := k.SetParams(ctx, params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateReserveAddressResponse{}, nil
}
