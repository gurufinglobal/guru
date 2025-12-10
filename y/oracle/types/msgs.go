package types

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// MsgUpdateModeratorAddress - methods for the message type.

// Route returns the message route.
func (msg MsgUpdateModeratorAddress) Route() string { return RouterKey }

// Type returns the message type.
func (msg MsgUpdateModeratorAddress) Type() string { return "update_moderator_address" }

// GetSigners returns the signers of the message.
func (msg MsgUpdateModeratorAddress) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes to verify the message signature.
func (msg MsgUpdateModeratorAddress) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation of the message.
func (msg MsgUpdateModeratorAddress) ValidateBasic() error {
	if msg.ModeratorAddress == "" || msg.NewModeratorAddress == "" {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, "addresses must be set")
	}
	if msg.ModeratorAddress == msg.NewModeratorAddress {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "new moderator must differ")
	}
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid moderator address: %v", err)
	}
	if _, err := sdk.AccAddressFromBech32(msg.NewModeratorAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid new moderator address: %v", err)
	}
	return nil
}

// MsgRegisterOracleRequest - methods for the message type.

// Route returns the message route.
func (msg MsgRegisterOracleRequest) Route() string { return RouterKey }

// Type returns the message type.
func (msg MsgRegisterOracleRequest) Type() string { return "register_oracle_request" }

// GetSigners returns the signers of the message.
func (msg MsgRegisterOracleRequest) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes to verify the message signature.
func (msg MsgRegisterOracleRequest) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation of the message.
func (msg MsgRegisterOracleRequest) ValidateBasic() error {
	if msg.ModeratorAddress == "" {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, "moderator address required")
	}
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid moderator address: %v", err)
	}
	req := OracleRequest{
		Category: msg.Category,
		Symbol:   msg.Symbol,
		Count:    int64(msg.Count),
		Period:   msg.Period,
		Status:   Status_STATUS_ACTIVE,
		Nonce:    0,
	}
	if err := req.ValidateBasic(); err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}
	return nil
}

// MsgUpdateOracleRequest - methods for the message type.

// Route returns the message route.
func (msg MsgUpdateOracleRequest) Route() string { return RouterKey }

// Type returns the message type.
func (msg MsgUpdateOracleRequest) Type() string { return "update_oracle_request" }

// GetSigners returns the signers of the message.
func (msg MsgUpdateOracleRequest) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes to verify the message signature.
func (msg MsgUpdateOracleRequest) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation of the message.
func (msg MsgUpdateOracleRequest) ValidateBasic() error {
	if msg.ModeratorAddress == "" {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, "moderator address required")
	}
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid moderator address: %v", err)
	}
	if msg.RequestId == 0 {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "request id must be set")
	}
	if msg.Count == 0 && msg.Period == 0 && msg.Status == Status_STATUS_UNSPECIFIED {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "no update field provided")
	}
	if msg.Status != Status_STATUS_UNSPECIFIED && msg.Status != Status_STATUS_ACTIVE && msg.Status != Status_STATUS_INACTIVE {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "invalid status")
	}
	return nil
}

// MsgSubmitOracleReport - methods for the message type.

// Route returns the message route.
func (msg MsgSubmitOracleReport) Route() string { return RouterKey }

// Type returns the message type.
func (msg MsgSubmitOracleReport) Type() string { return "submit_oracle_report" }

// GetSigners returns the signers of the message.
func (msg MsgSubmitOracleReport) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.ProviderAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes to verify the message signature.
func (msg MsgSubmitOracleReport) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation of the message.
func (msg MsgSubmitOracleReport) ValidateBasic() error {
	if msg.ProviderAddress == "" {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, "provider address required")
	}
	if _, err := sdk.AccAddressFromBech32(msg.ProviderAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid provider address: %v", err)
	}
	report := OracleReport{
		RequestId: msg.RequestId,
		Provider:  msg.ProviderAddress,
		RawData:   msg.RawData,
		Nonce:     msg.Nonce,
		Signature: msg.Signature,
	}
	if err := report.ValidateBasic(); err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, err.Error())
	}
	return nil
}

// MsgAddToWhitelist - methods for the message type.

// Route returns the message route.
func (msg MsgAddToWhitelist) Route() string { return RouterKey }

// Type returns the message type.
func (msg MsgAddToWhitelist) Type() string { return "add_to_whitelist" }

// GetSigners returns the signers of the message.
func (msg MsgAddToWhitelist) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes to verify the message signature.
func (msg MsgAddToWhitelist) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation of the message.
func (msg MsgAddToWhitelist) ValidateBasic() error {
	if msg.ModeratorAddress == "" || msg.Address == "" {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "addresses must be set")
	}
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid moderator address: %v", err)
	}
	if _, err := sdk.AccAddressFromBech32(msg.Address); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid whitelist address: %v", err)
	}
	return nil
}

// MsgRemoveFromWhitelist - methods for the message type.

// Route returns the message route.
func (msg MsgRemoveFromWhitelist) Route() string { return RouterKey }

// Type returns the message type.
func (msg MsgRemoveFromWhitelist) Type() string { return "remove_from_whitelist" }

// GetSigners returns the signers of the message.
func (msg MsgRemoveFromWhitelist) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes to verify the message signature.
func (msg MsgRemoveFromWhitelist) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation of the message.
func (msg MsgRemoveFromWhitelist) ValidateBasic() error {
	if msg.ModeratorAddress == "" || msg.Address == "" {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "addresses must be set")
	}
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid moderator address: %v", err)
	}
	if _, err := sdk.AccAddressFromBech32(msg.Address); err != nil {
		return errorsmod.Wrapf(errortypes.ErrInvalidAddress, "invalid whitelist address: %v", err)
	}
	return nil
}
