package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

var _ types.MsgServer = MsgServer{}

type MsgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) MsgServer {
	return MsgServer{keeper: keeper}
}

func (m MsgServer) UpdateParams(
	goCtx context.Context,
	req *types.MsgUpdateParams,
) (*types.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidParams.Wrap("empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := sdk.ValidateAuthority(ctx, m.keeper.GetAuthority(), req.GetAuthority()); err != nil {
		return nil, types.ErrInvalidAuthority.Wrap(err.Error())
	}
	if err := m.keeper.SetParams(ctx, req.GetParams()); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

func (m MsgServer) RetryRefund(
	goCtx context.Context,
	req *types.MsgRetryRefund,
) (*types.MsgRetryRefundResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRefundState.Wrap("empty request")
	}
	if _, err := sdk.AccAddressFromBech32(req.GetSigner()); err != nil {
		return nil, types.ErrRefundUnauthorized.Wrapf("invalid signer: %v", err)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	refund, err := m.keeper.RetryRefund(ctx, req.GetRefundId())
	if err != nil {
		return nil, err
	}
	return &types.MsgRetryRefundResponse{Refund: refund}, nil
}

func (m MsgServer) ClaimRefund(
	goCtx context.Context,
	req *types.MsgClaimRefund,
) (*types.MsgClaimRefundResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRefundState.Wrap("empty request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	refund, err := m.keeper.ClaimRefund(ctx, req.GetRefundId(), req.GetSigner())
	if err != nil {
		return nil, err
	}
	return &types.MsgClaimRefundResponse{Refund: refund}, nil
}
