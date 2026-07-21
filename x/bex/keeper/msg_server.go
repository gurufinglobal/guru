package keeper

import (
	"context"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

var _ types.MsgServer = MsgServer{}

type MsgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) MsgServer {
	return MsgServer{keeper: keeper}
}

func (m MsgServer) RegisterAdmin(ctx context.Context, req *types.MsgRegisterAdmin) (*types.MsgRegisterAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	return &types.MsgRegisterAdminResponse{}, m.keeper.RegisterAdmin(ctx, req.GetModerator(), req.GetAdminAddress())
}

func (m MsgServer) UpdateAdmin(ctx context.Context, req *types.MsgUpdateAdmin) (*types.MsgUpdateAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.keeper.UpdateAdmin(ctx, req.GetModerator(), req.GetOldAdminAddress(), req.GetNewAdminAddress()); err != nil {
		return nil, err
	}
	return &types.MsgUpdateAdminResponse{}, nil
}

func (m MsgServer) RemoveAdmin(ctx context.Context, req *types.MsgRemoveAdmin) (*types.MsgRemoveAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	return &types.MsgRemoveAdminResponse{}, m.keeper.RemoveAdmin(ctx, req.GetModerator(), req.GetAdminAddress())
}

func (m MsgServer) RegisterExchange(ctx context.Context, req *types.MsgRegisterExchange) (*types.MsgRegisterExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := m.keeper.RegisterExchange(ctx, req)
	if err != nil {
		return nil, err
	}
	return &types.MsgRegisterExchangeResponse{
		ExchangeId:     exchange.GetId(),
		ReserveAddress: exchange.GetReserveAddress(),
	}, nil
}

func (m MsgServer) UpdateExchange(ctx context.Context, req *types.MsgUpdateExchange) (*types.MsgUpdateExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := m.keeper.UpdateExchange(ctx, req.GetAdminAddress(), req.GetExchangeId(), req.GetExpectedRevision(), req.GetPatch())
	if err != nil {
		return nil, err
	}
	return &types.MsgUpdateExchangeResponse{Revision: exchange.GetRevision()}, nil
}

func (m MsgServer) DeleteExchange(ctx context.Context, req *types.MsgDeleteExchange) (*types.MsgDeleteExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	return &types.MsgDeleteExchangeResponse{}, m.keeper.DeleteExchange(ctx, req.GetAdminAddress(), req.GetExchangeId())
}

func (m MsgServer) AddReserveDepositor(ctx context.Context, req *types.MsgAddReserveDepositor) (*types.MsgAddReserveDepositorResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.keeper.AddReserveDepositor(ctx, req.GetAdminAddress(), req.GetExchangeId(), req.GetDepositorAddress()); err != nil {
		return nil, err
	}
	return &types.MsgAddReserveDepositorResponse{}, nil
}

func (m MsgServer) RemoveReserveDepositor(ctx context.Context, req *types.MsgRemoveReserveDepositor) (*types.MsgRemoveReserveDepositorResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.keeper.RemoveReserveDepositor(ctx, req.GetAdminAddress(), req.GetExchangeId(), req.GetDepositorAddress()); err != nil {
		return nil, err
	}
	return &types.MsgRemoveReserveDepositorResponse{}, nil
}

func (m MsgServer) DepositReserve(ctx context.Context, req *types.MsgDepositReserve) (*types.MsgDepositReserveResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	amount, err := protoCoinsToSDK(req.GetAmount())
	if err != nil {
		return nil, err
	}
	return &types.MsgDepositReserveResponse{}, m.keeper.DepositReserve(ctx, req.GetSender(), req.GetExchangeId(), amount)
}

func (m MsgServer) WithdrawReserve(ctx context.Context, req *types.MsgWithdrawReserve) (*types.MsgWithdrawReserveResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	amount, err := protoCoinsToSDK(req.GetAmount())
	if err != nil {
		return nil, err
	}
	_, recipient, err := m.keeper.canonicalAddress(req.GetRecipient())
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrapf("invalid recipient: %v", err)
	}
	return &types.MsgWithdrawReserveResponse{}, m.keeper.WithdrawReserve(ctx, req.GetAdminAddress(), req.GetExchangeId(), recipient, amount)
}

func (m MsgServer) WithdrawFees(ctx context.Context, req *types.MsgWithdrawFees) (*types.MsgWithdrawFeesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	amount, err := protoCoinsToSDK(req.GetAmount())
	if err != nil {
		return nil, err
	}
	_, recipient, err := m.keeper.canonicalAddress(req.GetRecipient())
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrapf("invalid recipient: %v", err)
	}
	if err := m.keeper.WithdrawFees(ctx, req.GetAdminAddress(), req.GetExchangeId(), recipient, amount); err != nil {
		return nil, err
	}
	return &types.MsgWithdrawFeesResponse{}, nil
}
