package keeper

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

var _ types.QueryServer = Keeper{}

// Params implements the Query/Params gRPC method.
func (k Keeper) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}

func (k Keeper) ModeratorAddress(c context.Context, _ *types.QueryModeratorAddressRequest) (*types.QueryModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	moderator, err := k.GetModeratorAddress(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryModeratorAddressResponse{ModeratorAddress: moderator}, nil
}

func (k Keeper) AdminAddress(c context.Context, _ *types.QueryAdminAddressRequest) (*types.QueryAdminAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryAdminAddressResponse{AdminAddress: params.AdminAddress}, nil
}

func (k Keeper) IsAdmin(c context.Context, req *types.QueryIsAdminRequest) (*types.QueryIsAdminResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryIsAdminResponse{IsAdmin: req.Address == params.AdminAddress}, nil
}

func (k Keeper) FeePercentage(c context.Context, _ *types.QueryFeePercentageRequest) (*types.QueryFeePercentageResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryFeePercentageResponse{FeePercentage: params.FeePercentage}, nil
}

func (k Keeper) ReserveAddress(c context.Context, _ *types.QueryReserveAddressRequest) (*types.QueryReserveAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryReserveAddressResponse{ReserveAddress: params.ReserveAddress}, nil
}

func (k Keeper) LockedFee(c context.Context, req *types.QueryLockedFeeRequest) (*types.QueryLockedFeeResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	lockedFee, found, err := k.GetLockedFee(ctx, req.SourcePort, req.SourceChannel, req.Sequence)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryLockedFeeResponse{Found: false}, nil
	}

	return &types.QueryLockedFeeResponse{
		Denom:  lockedFee.Denom,
		Amount: lockedFee.Amount.String(),
		Found:  true,
	}, nil
}

func (k Keeper) LockedFees(c context.Context, _ *types.QueryLockedFeesRequest) (*types.QueryLockedFeesResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	store := k.storeService.OpenKVStore(ctx)

	// Keys are stored as: locked_fee/{portID}/{channelID}/{sequence}
	prefix := []byte(types.LockedFeeKeyPrefix + "/")
	iterator, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	res := &types.QueryLockedFeesResponse{LockedFees: []types.LockedFee{}}

	for ; iterator.Valid(); iterator.Next() {
		key := string(iterator.Key())
		parts := strings.Split(key, "/")
		if len(parts) != 4 || parts[0] != types.LockedFeeKeyPrefix {
			return nil, fmt.Errorf("invalid locked fee key format: %q", key)
		}

		sequence, err := strconv.ParseUint(parts[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid locked fee sequence in key %q: %w", key, err)
		}

		amount := string(iterator.Value())
		if amount == "" {
			return nil, fmt.Errorf("empty locked fee value for key %q", key)
		}

		res.LockedFees = append(res.LockedFees, types.LockedFee{
			PortId:    parts[1],
			ChannelId: parts[2],
			Sequence:  sequence,
			Amount:    amount,
		})
	}

	return res, nil
}
