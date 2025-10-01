package keeper

import (
	"context"
	"encoding/json"
	"strings"

	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/GPTx-global/guru-v2/v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// MsgServer implementation
var _ types.MsgServer = &Keeper{}

// RegisterOracleRequestDoc defines a method for registering a new oracle request document
func (k Keeper) RegisterOracleRequestDoc(c context.Context, doc *types.MsgRegisterOracleRequestDoc) (*types.MsgRegisterOracleRequestDocResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	moderatorAddress := k.GetModeratorAddress(ctx)

	if moderatorAddress == "" {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "moderator address is not set")
	}
	if moderatorAddress != doc.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "moderator address is not authorized")
	}

	// Get the current count of oracle request documents
	count := k.GetOracleRequestDocCount(ctx)

	// Create a new oracle request document
	oracleRequestDoc := types.OracleRequestDoc{
		RequestId:       count + 1,
		Status:          doc.RequestDoc.Status,
		OracleType:      doc.RequestDoc.OracleType,
		Name:            doc.RequestDoc.Name,
		Description:     doc.RequestDoc.Description,
		Period:          doc.RequestDoc.Period,
		AccountList:     doc.RequestDoc.AccountList,
		Quorum:          doc.RequestDoc.Quorum,
		Endpoints:       doc.RequestDoc.Endpoints,
		AggregationRule: doc.RequestDoc.AggregationRule,
	}

	// Store the oracle request document
	k.SetOracleRequestDoc(ctx, oracleRequestDoc)

	// Increment the count
	k.SetOracleRequestDocCount(ctx, count+1)

	// Marshal the endpoints to a JSON string
	endpointsJson, err := json.Marshal(oracleRequestDoc.Endpoints)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "failed to marshal endpoints")
	}

	// Emit event for registering oracle request document
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRegisterOracleRequestDoc,
			sdk.NewAttribute(types.AttributeKeyRequestId, fmt.Sprint(oracleRequestDoc.RequestId)),
			sdk.NewAttribute(types.AttributeKeyOracleType, string(oracleRequestDoc.OracleType)),
			sdk.NewAttribute(types.AttributeKeyName, oracleRequestDoc.Name),
			sdk.NewAttribute(types.AttributeKeyDescription, oracleRequestDoc.Description),
			sdk.NewAttribute(types.AttributeKeyPeriod, fmt.Sprint(oracleRequestDoc.Period)),
			sdk.NewAttribute(types.AttributeKeyAccountList, strings.Join(oracleRequestDoc.AccountList, ",")),
			sdk.NewAttribute(types.AttributeKeyEndpoints, string(endpointsJson)),
			sdk.NewAttribute(types.AttributeKeyAggregationRule, string(oracleRequestDoc.AggregationRule)),
			sdk.NewAttribute(types.AttributeKeyStatus, string(oracleRequestDoc.Status)),
		),
	)

	return &types.MsgRegisterOracleRequestDocResponse{
		RequestId: oracleRequestDoc.RequestId,
	}, nil
}

// UpdateOracleRequestDoc defines a method for updating an existing oracle request document
func (k Keeper) UpdateOracleRequestDoc(c context.Context, doc *types.MsgUpdateOracleRequestDoc) (*types.MsgUpdateOracleRequestDocResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	moderatorAddress := k.GetModeratorAddress(ctx)

	if moderatorAddress == "" {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "moderator address is not set")
	}
	if moderatorAddress != doc.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "moderator address is not authorized")
	}

	err := k.updateOracleRequestDoc(ctx, doc.RequestDoc)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	// Marshal the endpoints to a JSON string
	endpointsJson, err := json.Marshal(doc.RequestDoc.Endpoints)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "failed to marshal endpoints")
	}

	// Emit event for updating oracle request document
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUpdateOracleRequestDoc,
			sdk.NewAttribute(types.AttributeKeyRequestId, fmt.Sprint(doc.RequestDoc.RequestId)),
			sdk.NewAttribute(types.AttributeKeyOracleType, string(doc.RequestDoc.OracleType)),
			sdk.NewAttribute(types.AttributeKeyName, doc.RequestDoc.Name),
			sdk.NewAttribute(types.AttributeKeyDescription, doc.RequestDoc.Description),
			sdk.NewAttribute(types.AttributeKeyPeriod, fmt.Sprint(doc.RequestDoc.Period)),
			sdk.NewAttribute(types.AttributeKeyAccountList, strings.Join(doc.RequestDoc.AccountList, ",")),
			sdk.NewAttribute(types.AttributeKeyEndpoints, string(endpointsJson)),
			sdk.NewAttribute(types.AttributeKeyAggregationRule, string(doc.RequestDoc.AggregationRule)),
			sdk.NewAttribute(types.AttributeKeyStatus, string(doc.RequestDoc.Status)),
			sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprint(doc.RequestDoc.Nonce)),
		),
	)

	return &types.MsgUpdateOracleRequestDocResponse{
		RequestId: doc.RequestDoc.RequestId,
	}, nil
}

// SubmitOracleData defines a method for submitting oracle data
func (k Keeper) SubmitOracleData(c context.Context, msg *types.MsgSubmitOracleData) (*types.MsgSubmitOracleDataResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	err := k.validateSubmitData(*msg.DataSet)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	requestId := msg.DataSet.RequestId

	requestDoc, err := k.GetOracleRequestDoc(ctx, requestId)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "request document not found")
	}

	if requestDoc == nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "request document not found")
	}

	accountList := requestDoc.AccountList
	fromAddress := msg.AuthorityAddress

	isAuthorized := k.checkAccountAuthorized(accountList, fromAddress)
	if !isAuthorized {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "account is not authorized")
	}

	nonce := requestDoc.GetNonce()

	if msg.DataSet.Nonce != nonce+1 {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "nonce is not correct")
	}

	k.SetSubmitData(ctx, *msg.DataSet)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSubmitOracleData,
			sdk.NewAttribute(types.AttributeKeyRequestId, fmt.Sprint(requestId)),
			sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprint(msg.DataSet.Nonce)),
			sdk.NewAttribute(types.AttributeKeyRawData, msg.DataSet.RawData),
			sdk.NewAttribute(types.AttributeKeyFromAddress, fromAddress),
		),
	)

	return &types.MsgSubmitOracleDataResponse{}, nil

}

// UpdateModeratorAddress defines a method for updating the moderator address
func (k Keeper) UpdateModeratorAddress(c context.Context, msg *types.MsgUpdateModeratorAddress) (*types.MsgUpdateModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	currentModeratorAddress := k.GetModeratorAddress(ctx)

	if currentModeratorAddress != msg.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "from address is different from current moderator address")
	}
	if currentModeratorAddress == "" {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "moderator address is not set")
	}
	if currentModeratorAddress == msg.NewModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "new moderator address is same as current moderator address")
	}

	k.SetModeratorAddress(ctx, msg.ModeratorAddress)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUpdateModeratorAddress,
			sdk.NewAttribute(types.AttributeKeyModeratorAddress, msg.ModeratorAddress),
		),
	)

	return &types.MsgUpdateModeratorAddressResponse{}, nil
}
