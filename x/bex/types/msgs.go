package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// bex message types
const (
	TypeMsgRegisterAdmin    = "register_admin"
	TypeMsgRemoveAdmin      = "remove_admin"
	TypeMsgRegisterExchange = "register_exchange"
	TypeMsgUpdateExchange   = "update_exchange"
	TypeMsgUpdateRatemeter  = "update_ratemeter"
	TypeMsgWithdrawFees     = "withdraw_fees"
	TypeMsgChangeModerator  = "change_moderator"
)

// MsgRegisterAdmin
var _ sdk.Msg = &MsgRegisterAdmin{}

// NewMsgRegisterAdmin - construct a msg.
func NewMsgRegisterAdmin(modAddress, adminAddress sdk.AccAddress, exchangeID math.Int) *MsgRegisterAdmin {
	return &MsgRegisterAdmin{
		ModeratorAddress: modAddress.String(),
		AdminAddress:     adminAddress.String(),
		ExchangeId:       exchangeID,
	}
}

// Route Implements Msg.
func (msg MsgRegisterAdmin) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgRegisterAdmin) Type() string { return TypeMsgRegisterAdmin }

// ValidateBasic Implements Msg.
func (msg MsgRegisterAdmin) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin address, %s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgRegisterAdmin) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgRegisterAdmin) GetSigners() []sdk.AccAddress {
	modAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{modAddress}
}

// MsgRemoveAdmin
var _ sdk.Msg = &MsgRemoveAdmin{}

// NewMsgRemoveAdmin - construct a msg.
func NewMsgRemoveAdmin(modAddress, adminAddress sdk.AccAddress) *MsgRemoveAdmin {
	return &MsgRemoveAdmin{
		ModeratorAddress: modAddress.String(),
		AdminAddress:     adminAddress.String(),
	}
}

// Route Implements Msg.
func (msg MsgRemoveAdmin) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgRemoveAdmin) Type() string { return TypeMsgRemoveAdmin }

// ValidateBasic Implements Msg.
func (msg MsgRemoveAdmin) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin address, %s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgRemoveAdmin) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgRemoveAdmin) GetSigners() []sdk.AccAddress {
	modAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{modAddress}
}

// MsgRegisterExchange
var _ sdk.Msg = &MsgRegisterExchange{}

// NewMsgRegisterExchange - construct a msg.
func NewMsgRegisterExchange(adminAddress sdk.AccAddress, exchange *Exchange) *MsgRegisterExchange {
	return &MsgRegisterExchange{
		AdminAddress: adminAddress.String(),
		Exchange:     exchange,
	}
}

// Route Implements Msg.
func (msg MsgRegisterExchange) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgRegisterExchange) Type() string { return TypeMsgRegisterExchange }

// ValidateBasic Implements Msg.
func (msg MsgRegisterExchange) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin address, %s", err)
	}

	if err := msg.Exchange.Validate(); err != nil {
		return errorsmod.Wrapf(ErrInvalidExchange, "%s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgRegisterExchange) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgRegisterExchange) GetSigners() []sdk.AccAddress {
	adminAddress, _ := sdk.AccAddressFromBech32(msg.AdminAddress)
	return []sdk.AccAddress{adminAddress}
}

// MsgUpdateExchange
var _ sdk.Msg = &MsgUpdateExchange{}

// NewMsgUpdateExchange - construct a msg.
func NewMsgUpdateExchange(adminAddress sdk.AccAddress, exchangeID math.Int, key, value string) *MsgUpdateExchange {
	return &MsgUpdateExchange{
		AdminAddress: adminAddress.String(),
		ExchangeId:   exchangeID,
		Key:          key,
		Value:        value,
	}
}

// Route Implements Msg.
func (msg MsgUpdateExchange) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgUpdateExchange) Type() string { return TypeMsgUpdateExchange }

// ValidateBasic Implements Msg.
func (msg MsgUpdateExchange) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin address, %s", err)
	}

	if err := ValidateExchangeID(msg.ExchangeId); err != nil {
		return errorsmod.Wrapf(ErrInvalidExchange, "%s", err)
	}

	if err := ValidateExchangeUpdateKeys(msg.Key); err != nil {
		return errorsmod.Wrapf(ErrInvalidKey, "%s", err)
	}

	if msg.Value == "" {
		return errorsmod.Wrapf(ErrInvalidValue, " value is empty")
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgUpdateExchange) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgUpdateExchange) GetSigners() []sdk.AccAddress {
	authAddress, _ := sdk.AccAddressFromBech32(msg.AdminAddress)
	return []sdk.AccAddress{authAddress}
}

// MsgUpdateRatemeter
var _ sdk.Msg = &MsgUpdateRatemeter{}

// NewMsgUpdateRatemeter - construct a msg.
func NewMsgUpdateRatemeter(moderatorAddress sdk.AccAddress, ratemeter *Ratemeter) *MsgUpdateRatemeter {
	return &MsgUpdateRatemeter{
		ModeratorAddress: moderatorAddress.String(),
		Ratemeter:        ratemeter,
	}
}

// Route Implements Msg.
func (msg MsgUpdateRatemeter) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgUpdateRatemeter) Type() string { return TypeMsgUpdateRatemeter }

// ValidateBasic Implements Msg.
func (msg MsgUpdateRatemeter) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}

	if err := ValidateRatemeter(msg.Ratemeter); err != nil {
		return errorsmod.Wrapf(ErrInvalidRatemeter, "%s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgUpdateRatemeter) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgUpdateRatemeter) GetSigners() []sdk.AccAddress {
	moderatorAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{moderatorAddress}
}

// MsgWithdrawFees
var _ sdk.Msg = &MsgWithdrawFees{}

// NewMsgWithdrawFees - construct a msg.
func NewMsgWithdrawFees(adminAddress sdk.AccAddress, exchangeID math.Int, withdrawAddress sdk.AccAddress) *MsgWithdrawFees {
	return &MsgWithdrawFees{
		AdminAddress:    adminAddress.String(),
		ExchangeId:      exchangeID,
		WithdrawAddress: withdrawAddress.String(),
	}
}

// Route Implements Msg.
func (msg MsgWithdrawFees) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgWithdrawFees) Type() string { return TypeMsgWithdrawFees }

// ValidateBasic Implements Msg.
func (msg MsgWithdrawFees) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.AdminAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin address, %s", err)
	}

	if err := ValidateExchangeID(msg.ExchangeId); err != nil {
		return errorsmod.Wrapf(ErrInvalidExchange, "%s", err)
	}

	if _, err := sdk.AccAddressFromBech32(msg.WithdrawAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " withdraw address, %s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgWithdrawFees) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgWithdrawFees) GetSigners() []sdk.AccAddress {
	adminAddress, _ := sdk.AccAddressFromBech32(msg.AdminAddress)
	return []sdk.AccAddress{adminAddress}
}

// MsgChangeModerator
var _ sdk.Msg = &MsgChangeBexModerator{}

// NewMsgChangeModerator - construct a msg.
func NewMsgChangeModerator(modAddress, newModeratorAddress sdk.AccAddress) *MsgChangeBexModerator {
	return &MsgChangeBexModerator{ModeratorAddress: modAddress.String(), NewModeratorAddress: newModeratorAddress.String()}
}

// Route Implements Msg.
func (msg MsgChangeBexModerator) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgChangeBexModerator) Type() string { return TypeMsgChangeModerator }

// ValidateBasic Implements Msg.
func (msg MsgChangeBexModerator) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}

	if _, err := sdk.AccAddressFromBech32(msg.NewModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " new moderator address, %s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgChangeBexModerator) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgChangeBexModerator) GetSigners() []sdk.AccAddress {
	authAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{authAddress}
}
