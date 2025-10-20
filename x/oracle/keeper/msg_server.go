package keeper

import (
	"context"
	"encoding/json"
	"strings"

	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/gurufinglobal/guru/v2/x/oracle/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/crypto"
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

	// Validate the oracle request document with current parameters
	params := k.GetParams(ctx)
	err := oracleRequestDoc.ValidateWithParams(params)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	// Store the oracle request document
	k.SetOracleRequestDoc(ctx, oracleRequestDoc)

	// Increment the count
	k.SetOracleRequestDocCount(ctx, count+1)

	// Marshal the endpoints to a JSON string
	endpointsJson, _ := json.Marshal(oracleRequestDoc.Endpoints)

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
	endpointsJson, _ := json.Marshal(doc.RequestDoc.Endpoints)

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

	// Validate that DataSet is provided
	if msg.DataSet == nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "DataSet must be provided")
	}

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

	// Check if RequestDoc status is ENABLED
	if requestDoc.Status != types.RequestStatus_REQUEST_STATUS_ENABLED {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "request document is not enabled")
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

	err = k.verifySubmitData(ctx, msg)
	if err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
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

// UpdateParams defines a method for updating oracle module parameters
func (k Keeper) UpdateParams(c context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	// Validate the authority address
	if k.authority != msg.Authority {
		return nil, errorsmod.Wrapf(errortypes.ErrUnauthorized, "invalid authority; expected %s, got %s", k.authority, msg.Authority)
	}

	// Validate the new parameters
	if err := msg.Params.Validate(); err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	// Update the parameters
	if err := k.SetParams(ctx, msg.Params); err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"oracle_params_updated",
			sdk.NewAttribute("authority", msg.Authority),
			sdk.NewAttribute("submit_window", fmt.Sprintf("%d", msg.Params.SubmitWindow)),
			sdk.NewAttribute("min_submit_per_window", msg.Params.MinSubmitPerWindow.String()),
			sdk.NewAttribute("slash_fraction_downtime", msg.Params.SlashFractionDowntime.String()),
		),
	)

	return &types.MsgUpdateParamsResponse{}, nil
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

	k.SetModeratorAddress(ctx, msg.NewModeratorAddress)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUpdateModeratorAddress,
			sdk.NewAttribute(types.AttributeKeyModeratorAddress, msg.NewModeratorAddress),
		),
	)

	return &types.MsgUpdateModeratorAddressResponse{}, nil
}

func (k Keeper) verifySubmitData(ctx context.Context, msg *types.MsgSubmitOracleData) error {
	if msg == nil || msg.DataSet == nil {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "missing dataset")
	}

	signBytes, err := msg.DataSet.Bytes()
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	sig := msg.DataSet.Signature
	if len(sig) != crypto.SignatureLength {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "invalid signature length")
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	if sig[64] != 0 && sig[64] != 1 {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "invalid signature recovery id")
	}

	providerAcc, err := sdk.AccAddressFromBech32(msg.DataSet.Provider)
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, "invalid provider address")
	}

	acc := k.accountKeeper.GetAccount(ctx, providerAcc)
	if acc == nil {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "account not found")
	}
	if acc.GetPubKey() == nil {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "public key not found")
	}

	if !acc.GetPubKey().VerifySignature(signBytes, sig) {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "invalid dataset signature")
	}

	return nil
}
