package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
)

var _ constitutionv1.MsgServer = MsgServer{}

type MsgServer struct {
	constitutionv1.UnimplementedMsgServer

	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) MsgServer {
	return MsgServer{keeper: keeper}
}

func (m MsgServer) UpdateParams(goCtx context.Context, req *constitutionv1.MsgUpdateParams) (*constitutionv1.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateAuthority(ctx, req.Authority); err != nil {
		return nil, constitutiontypes.ErrInvalidAuthority.Wrap(err.Error())
	}
	if err := m.keeper.UpdateParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &constitutionv1.MsgUpdateParamsResponse{}, nil
}

func (m MsgServer) UpdateBaseAddress(goCtx context.Context, req *constitutionv1.MsgUpdateBaseAddress) (*constitutionv1.MsgUpdateBaseAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.Moderator); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateBaseAddress(ctx, req.BaseAddress); err != nil {
		return nil, err
	}

	return &constitutionv1.MsgUpdateBaseAddressResponse{}, nil
}

func (m MsgServer) UpdateModeratorAddress(goCtx context.Context, req *constitutionv1.MsgUpdateModeratorAddress) (*constitutionv1.MsgUpdateModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.Moderator); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateModeratorAddress(ctx, req.ModeratorAddress); err != nil {
		return nil, err
	}

	return &constitutionv1.MsgUpdateModeratorAddressResponse{}, nil
}

func (m MsgServer) UpdateSeparationRatio(goCtx context.Context, req *constitutionv1.MsgUpdateSeparationRatio) (*constitutionv1.MsgUpdateSeparationRatioResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, constitutiontypes.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.Moderator); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateSeparationRatio(ctx, req.SeparationRatio); err != nil {
		return nil, err
	}

	return &constitutionv1.MsgUpdateSeparationRatioResponse{}, nil
}

func (m MsgServer) validateAuthority(ctx sdk.Context, authority string) error {
	expectedAuthority, err := m.keeper.AuthorityAddressString()
	if err != nil {
		return err
	}

	return sdk.ValidateAuthority(ctx, expectedAuthority, authority)
}

func (m MsgServer) validateModerator(ctx sdk.Context, moderator string) error {
	currentModerator, err := m.keeper.GetModeratorAddress(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return constitutiontypes.ErrInvalidAuthority.Wrap("moderator_address is not initialized")
		}
		return err
	}

	if err := sdk.ValidateAuthority(ctx, currentModerator, moderator); err != nil {
		return constitutiontypes.ErrInvalidAuthority.Wrap(err.Error())
	}

	return nil
}
