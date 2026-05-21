package keeper

import (
	"context"

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
	if err := sdk.ValidateAuthority(ctx, m.keeper.authority.String(), req.Authority); err != nil {
		return nil, constitutiontypes.ErrInvalidAuthority.Wrap(err.Error())
	}
	if err := m.keeper.UpdateParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &constitutionv1.MsgUpdateParamsResponse{}, nil
}
