package keeper

import (
	"context"

	"github.com/GPTx-global/guru-v2/v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ types.QueryServer = Keeper{}

// Parameters queries the parameters of the module
func (k Keeper) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params := k.GetParams(sdk.UnwrapSDKContext(ctx))
	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}

// OracleData queries oracle data by ID
func (k Keeper) OracleData(ctx context.Context, req *types.QueryOracleDataRequest) (*types.QueryOracleDataResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	doc, err := k.GetOracleRequestDoc(sdkCtx, req.RequestId)
	if err != nil {
		return nil, err
	}
	dataSet, err := k.GetDataSet(sdkCtx, req.RequestId, doc.Nonce)
	if err != nil {
		return nil, err
	}
	return &types.QueryOracleDataResponse{
		DataSet: dataSet,
	}, nil
}

// OracleRequestDoc queries oracle request doc by ID
func (k Keeper) OracleRequestDoc(ctx context.Context, req *types.QueryOracleRequestDocRequest) (*types.QueryOracleRequestDocResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	doc, err := k.GetOracleRequestDoc(sdkCtx, req.RequestId)
	if err != nil {
		return nil, err
	}
	return &types.QueryOracleRequestDocResponse{
		RequestDoc: *doc,
	}, nil
}

// OracleRequestDocs queries an oracle request document list
func (k Keeper) OracleRequestDocs(ctx context.Context, req *types.QueryOracleRequestDocsRequest) (*types.QueryOracleRequestDocsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var docs []*types.OracleRequestDoc
	if req.Status != types.RequestStatus_REQUEST_STATUS_UNSPECIFIED {
		docs = k.GetOracleRequestDocsByStatus(sdkCtx, req.Status)
	} else {
		docs = k.GetOracleRequestDocs(sdkCtx)
	}
	return &types.QueryOracleRequestDocsResponse{
		OracleRequestDocs: docs,
	}, nil
}

// GetModeratorAddress queries the moderator address
func (k Keeper) ModeratorAddress(ctx context.Context, req *types.QueryModeratorAddressRequest) (*types.QueryModeratorAddressResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	address := k.GetModeratorAddress(sdkCtx)
	return &types.QueryModeratorAddressResponse{
		ModeratorAddress: address,
	}, nil
}

func (k Keeper) OracleSubmitData(ctx context.Context, req *types.QueryOracleSubmitDataRequest) (*types.QueryOracleSubmitDataResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	submitDatas, err := k.GetSubmitData(sdkCtx, req.RequestId, req.Nonce, req.Provider)
	if err != nil {
		return nil, err
	}
	return &types.QueryOracleSubmitDataResponse{
		SubmitDatas: submitDatas,
	}, nil
}
