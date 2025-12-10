package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gurufinglobal/guru/v2/y/oracle/types"
)

var _ types.MsgServer = Keeper{}

// UpdateModeratorAddress updates the moderator address.
func (k Keeper) UpdateModeratorAddress(goCtx context.Context, msg *types.MsgUpdateModeratorAddress) (*types.MsgUpdateModeratorAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	current := k.GetModeratorAddress(ctx)
	if current == "" {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "moderator not set")
	}
	if current != msg.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "unauthorized moderator")
	}
	if msg.ModeratorAddress == msg.NewModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "new moderator must differ")
	}

	k.SetModeratorAddress(ctx, msg.NewModeratorAddress)

	k.Logger(ctx).Info("moderator address updated",
		"old", msg.ModeratorAddress,
		"new", msg.NewModeratorAddress,
	)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUpdateModerator,
			sdk.NewAttribute(types.AttributeKeyModerator, msg.NewModeratorAddress),
		),
	)
	return &types.MsgUpdateModeratorAddressResponse{}, nil
}

// RegisterOracleRequest registers a new oracle request.
func (k Keeper) RegisterOracleRequest(goCtx context.Context, msg *types.MsgRegisterOracleRequest) (*types.MsgRegisterOracleRequestResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)
	if !params.Enable {
		return nil, errorsmod.Wrap(types.ErrModuleDisabled, "oracle disabled")
	}

	moderator := k.GetModeratorAddress(ctx)
	if moderator == "" {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "moderator not set")
	}
	if moderator != msg.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "unauthorized moderator")
	}

	requestID := k.NextRequestID(ctx)
	req := types.OracleRequest{
		Id:       requestID,
		Category: msg.Category,
		Symbol:   msg.Symbol,
		Count:    int64(msg.Count),
		Period:   msg.Period,
		Status:   types.Status_STATUS_ACTIVE,
		Nonce:    0,
	}
	if err := req.ValidateBasic(); err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	k.SetRequest(ctx, req)
	k.IncrementRequestCount(ctx)
	k.SetCategory(ctx, msg.Category)

	k.Logger(ctx).Info("oracle request registered",
		"request_id", requestID,
		"category", req.Category.String(),
		"symbol", req.Symbol,
		"count", req.Count,
		"period", req.Period,
	)

	// Emit single EventOracleTask for daemon to start working
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeOracleTask,
			sdk.NewAttribute(types.AttributeKeyRequestID, strconv.FormatUint(requestID, 10)),
		),
	)

	return &types.MsgRegisterOracleRequestResponse{RequestId: requestID}, nil
}

// UpdateOracleRequest updates an existing oracle request.
func (k Keeper) UpdateOracleRequest(goCtx context.Context, msg *types.MsgUpdateOracleRequest) (*types.MsgUpdateOracleRequestResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)
	if !params.Enable {
		return nil, errorsmod.Wrap(types.ErrModuleDisabled, "oracle disabled")
	}

	moderator := k.GetModeratorAddress(ctx)
	if moderator == "" {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "moderator not set")
	}
	if moderator != msg.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "unauthorized moderator")
	}

	req, found := k.GetRequest(ctx, msg.RequestId)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrRequestNotFound, "id %d", msg.RequestId)
	}

	if msg.Count != 0 {
		req.Count = int64(msg.Count)
	}
	if msg.Period != 0 {
		req.Period = msg.Period
	}
	if msg.Status != types.Status_STATUS_UNSPECIFIED {
		req.Status = msg.Status
	}

	if err := req.ValidateBasic(); err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	k.SetRequest(ctx, req)

	k.Logger(ctx).Info("oracle request updated",
		"request_id", req.Id,
		"count", req.Count,
		"period", req.Period,
		"status", req.Status.String(),
	)

	return &types.MsgUpdateOracleRequestResponse{}, nil
}

// SubmitOracleReport submits a new oracle report.
func (k Keeper) SubmitOracleReport(goCtx context.Context, msg *types.MsgSubmitOracleReport) (*types.MsgSubmitOracleReportResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)
	if !params.Enable {
		return nil, errorsmod.Wrap(types.ErrModuleDisabled, "oracle disabled")
	}

	if !k.IsWhitelisted(ctx, msg.ProviderAddress) {
		k.Logger(ctx).Warn("report rejected: provider not whitelisted", "provider", msg.ProviderAddress)
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "provider not whitelisted")
	}

	req, found := k.GetRequest(ctx, msg.RequestId)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrRequestNotFound, "id %d", msg.RequestId)
	}
	if req.Status != types.Status_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, "request is not active")
	}

	if msg.Nonce != req.Nonce+1 {
		k.Logger(ctx).Warn("report rejected: invalid nonce",
			"request_id", msg.RequestId,
			"expected_nonce", req.Nonce+1,
			"received_nonce", msg.Nonce,
		)
		return nil, errorsmod.Wrapf(types.ErrInvalidNonce, "expected nonce %d", req.Nonce+1)
	}

	if _, exists := k.GetReport(ctx, msg.RequestId, msg.Nonce, msg.ProviderAddress); exists {
		return nil, errorsmod.Wrap(types.ErrReportExists, "duplicate report")
	}

	report := types.OracleReport{
		RequestId: msg.RequestId,
		Provider:  msg.ProviderAddress,
		RawData:   msg.RawData,
		Nonce:     msg.Nonce,
		Signature: msg.Signature,
	}
	if err := report.ValidateBasic(); err != nil {
		return nil, errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	if err := k.verifyReportSignature(ctx, report); err != nil {
		k.Logger(ctx).Warn("report rejected: signature verification failed",
			"request_id", msg.RequestId,
			"provider", msg.ProviderAddress,
		)
		return nil, err
	}

	k.SetReport(ctx, report)

	k.Logger(ctx).Debug("report submitted",
		"request_id", report.RequestId,
		"nonce", report.Nonce,
		"provider", report.Provider,
	)

	return &types.MsgSubmitOracleReportResponse{}, nil
}

// AddToWhitelist adds a new provider to the whitelist.
func (k Keeper) AddToWhitelist(goCtx context.Context, msg *types.MsgAddToWhitelist) (*types.MsgAddToWhitelistResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if k.GetModeratorAddress(ctx) != msg.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "unauthorized moderator")
	}
	k.addToWhitelist(ctx, msg.Address)

	k.Logger(ctx).Info("provider added to whitelist",
		"address", msg.Address,
		"total_count", k.GetWhitelistCount(ctx),
	)

	return &types.MsgAddToWhitelistResponse{}, nil
}

// RemoveFromWhitelist removes a provider from the whitelist.
func (k Keeper) RemoveFromWhitelist(goCtx context.Context, msg *types.MsgRemoveFromWhitelist) (*types.MsgRemoveFromWhitelistResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if k.GetModeratorAddress(ctx) != msg.ModeratorAddress {
		return nil, errorsmod.Wrap(errortypes.ErrUnauthorized, "unauthorized moderator")
	}
	k.removeFromWhitelist(ctx, msg.Address)

	k.Logger(ctx).Info("provider removed from whitelist",
		"address", msg.Address,
		"total_count", k.GetWhitelistCount(ctx),
	)

	return &types.MsgRemoveFromWhitelistResponse{}, nil
}

// verifyReportSignature verifies the signature of an oracle report.
func (k Keeper) verifyReportSignature(ctx sdk.Context, report types.OracleReport) error {
	signBytes, err := report.Bytes()
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}

	sig := report.Signature
	if len(sig) != crypto.SignatureLength {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "invalid signature length")
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	if sig[64] != 0 && sig[64] != 1 {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "invalid recovery id")
	}

	providerAcc, err := sdk.AccAddressFromBech32(report.Provider)
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, "invalid provider address")
	}

	acc := k.accountKeeper.GetAccount(ctx, providerAcc)
	if acc == nil || acc.GetPubKey() == nil {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "provider account/pubkey not found")
	}

	if !acc.GetPubKey().VerifySignature(signBytes, sig) {
		return errorsmod.Wrap(errortypes.ErrUnauthorized, "invalid report signature")
	}

	return nil
}
