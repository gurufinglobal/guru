package keeper

import (
	"context"

	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

var _ bexv1.MsgServer = MsgServer{}

type MsgServer struct {
	bexv1.UnimplementedMsgServer
	keeper *Keeper
}

func NewMsgServer(keeper *Keeper) MsgServer {
	return MsgServer{keeper: keeper}
}

func (m MsgServer) RegisterAdmin(ctx context.Context, req *bexv1.MsgRegisterAdmin) (*bexv1.MsgRegisterAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	return &bexv1.MsgRegisterAdminResponse{}, m.keeper.RegisterAdmin(ctx, req.GetModerator(), req.GetAdminAddress())
}

func (m MsgServer) RemoveAdmin(ctx context.Context, req *bexv1.MsgRemoveAdmin) (*bexv1.MsgRemoveAdminResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	return &bexv1.MsgRemoveAdminResponse{}, m.keeper.RemoveAdmin(ctx, req.GetModerator(), req.GetAdminAddress())
}

func (m MsgServer) RegisterExchange(ctx context.Context, req *bexv1.MsgRegisterExchange) (*bexv1.MsgRegisterExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := m.keeper.RegisterExchange(ctx, req)
	if err != nil {
		return nil, err
	}
	return &bexv1.MsgRegisterExchangeResponse{
		ExchangeId:     exchange.GetId(),
		ReserveAddress: exchange.GetReserveAddress(),
	}, nil
}

func (m MsgServer) UpdateExchange(ctx context.Context, req *bexv1.MsgUpdateExchange) (*bexv1.MsgUpdateExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	exchange, err := m.keeper.UpdateExchange(ctx, req.GetAdminAddress(), req.GetExchangeId(), req.GetExpectedRevision(), req.GetPatch())
	if err != nil {
		return nil, err
	}
	return &bexv1.MsgUpdateExchangeResponse{Revision: exchange.GetRevision()}, nil
}

func (m MsgServer) DeleteExchange(ctx context.Context, req *bexv1.MsgDeleteExchange) (*bexv1.MsgDeleteExchangeResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	return &bexv1.MsgDeleteExchangeResponse{}, m.keeper.DeleteExchange(ctx, req.GetAdminAddress(), req.GetExchangeId())
}

func (m MsgServer) AddReserveDepositor(ctx context.Context, req *bexv1.MsgAddReserveDepositor) (*bexv1.MsgAddReserveDepositorResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.keeper.AddReserveDepositor(ctx, req.GetAdminAddress(), req.GetExchangeId(), req.GetDepositorAddress()); err != nil {
		return nil, err
	}
	return &bexv1.MsgAddReserveDepositorResponse{}, nil
}

func (m MsgServer) RemoveReserveDepositor(ctx context.Context, req *bexv1.MsgRemoveReserveDepositor) (*bexv1.MsgRemoveReserveDepositorResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := m.keeper.RemoveReserveDepositor(ctx, req.GetAdminAddress(), req.GetExchangeId(), req.GetDepositorAddress()); err != nil {
		return nil, err
	}
	return &bexv1.MsgRemoveReserveDepositorResponse{}, nil
}

func (m MsgServer) DepositReserve(ctx context.Context, req *bexv1.MsgDepositReserve) (*bexv1.MsgDepositReserveResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	amount, err := protoCoinsToSDK(req.GetAmount())
	if err != nil {
		return nil, err
	}
	return &bexv1.MsgDepositReserveResponse{}, m.keeper.DepositReserve(ctx, req.GetSender(), req.GetExchangeId(), amount)
}

func (m MsgServer) WithdrawReserve(ctx context.Context, req *bexv1.MsgWithdrawReserve) (*bexv1.MsgWithdrawReserveResponse, error) {
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
	return &bexv1.MsgWithdrawReserveResponse{}, m.keeper.WithdrawReserve(ctx, req.GetAdminAddress(), req.GetExchangeId(), recipient, amount)
}

func (m MsgServer) WithdrawFees(ctx context.Context, req *bexv1.MsgWithdrawFees) (*bexv1.MsgWithdrawFeesResponse, error) {
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
	return &bexv1.MsgWithdrawFeesResponse{}, nil
}
