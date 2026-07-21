package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gurufinglobal/guru/v3/x/oracle/types"
)

var _ types.MsgServer = MsgServer{}

type MsgServer struct {
	types.UnimplementedMsgServer

	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) MsgServer {
	return MsgServer{keeper: keeper}
}

func (m MsgServer) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.GetModerator()); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateParams(ctx, req.GetParams()); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

func (m MsgServer) UpsertTask(goCtx context.Context, req *types.MsgUpsertTask) (*types.MsgUpsertTaskResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.GetModerator()); err != nil {
		return nil, err
	}
	if err := m.keeper.SetTask(ctx, req.GetTask()); err != nil {
		return nil, err
	}

	return &types.MsgUpsertTaskResponse{}, nil
}

func (m MsgServer) RemoveTask(goCtx context.Context, req *types.MsgRemoveTask) (*types.MsgRemoveTaskResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.validateModerator(ctx, req.GetModerator()); err != nil {
		return nil, err
	}
	if err := m.keeper.RemoveTask(ctx, req.GetSymbol()); err != nil {
		return nil, err
	}

	return &types.MsgRemoveTaskResponse{}, nil
}

func (m MsgServer) validateModerator(ctx sdk.Context, moderator string) error {
	if _, err := m.keeper.accountCodec.StringToBytes(moderator); err != nil {
		return types.ErrInvalidAuthority.Wrapf("invalid moderator: %v", err)
	}

	currentModerator, err := m.keeper.constitutionKeeper.GetModeratorAddress(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrInvalidAuthority.Wrap("moderator_address is not initialized")
		}
		return err
	}

	if err := sdk.ValidateAuthority(ctx, currentModerator, moderator); err != nil {
		return types.ErrInvalidAuthority.Wrap(err.Error())
	}

	return nil
}
