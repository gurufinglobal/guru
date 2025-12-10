package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// cex message types
const (
	TypeMsgRegisterDiscounts = ModuleName + "_register_discounts"
	TypeMsgRemoveDiscounts   = ModuleName + "_remove_discounts"
	TypeMsgChangeModerator   = ModuleName + "_change_moderator_address"
)

var _ sdk.Msg = &MsgChangeModerator{}

// NewMsgChangeModerator - construct a msg.
func NewMsgChangeModerator(modAddress, newModeratorAddress sdk.AccAddress) *MsgChangeModerator {
	return &MsgChangeModerator{ModeratorAddress: modAddress.String(), NewModeratorAddress: newModeratorAddress.String()}
}

// Route Implements Msg.
func (msg MsgChangeModerator) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgChangeModerator) Type() string { return TypeMsgChangeModerator }

// ValidateBasic Implements Msg.
func (msg MsgChangeModerator) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}

	if _, err := sdk.AccAddressFromBech32(msg.NewModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " new moderator address, %s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgChangeModerator) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgChangeModerator) GetSigners() []sdk.AccAddress {
	authAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{authAddress}
}

var _ sdk.Msg = &MsgRegisterDiscounts{}

// NewMsgRegisterDiscounts - construct a msg.
func NewMsgRegisterDiscounts(moderatorAddress sdk.AccAddress, discounts []AccountDiscount) *MsgRegisterDiscounts {
	return &MsgRegisterDiscounts{
		ModeratorAddress: moderatorAddress.String(),
		Discounts:        discounts,
	}
}

// Route Implements Msg.
func (msg MsgRegisterDiscounts) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgRegisterDiscounts) Type() string { return TypeMsgRegisterDiscounts }

// ValidateBasic Implements Msg.
func (msg MsgRegisterDiscounts) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}

	if len(msg.Discounts) == 0 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, " no discounts provided")
	}
	for _, discount := range msg.Discounts {
		if err := ValidateAccountDiscount(discount); err != nil {
			return err
		}
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgRegisterDiscounts) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgRegisterDiscounts) GetSigners() []sdk.AccAddress {
	authAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{authAddress}
}

var _ sdk.Msg = &MsgRemoveDiscounts{}

// NewMsgRemoveDiscounts - construct a msg.
func NewMsgRemoveDiscounts(modAddress, address sdk.AccAddress, module string, msgType string) *MsgRemoveDiscounts {
	return &MsgRemoveDiscounts{
		ModeratorAddress: modAddress.String(),
		Address:          address.String(),
		Module:           module,
		MsgType:          msgType,
	}
}

// Route Implements Msg.
func (msg MsgRemoveDiscounts) Route() string { return RouterKey }

// Type Implements Msg.
func (msg MsgRemoveDiscounts) Type() string { return TypeMsgRemoveDiscounts }

// ValidateBasic Implements Msg.
func (msg MsgRemoveDiscounts) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.ModeratorAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " moderator address, %s", err)
	}
	if _, err := sdk.AccAddressFromBech32(msg.Address); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " discount address, %s", err)
	}

	return nil
}

// GetSignBytes Implements Msg.
func (msg MsgRemoveDiscounts) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners Implements Msg.
func (msg MsgRemoveDiscounts) GetSigners() []sdk.AccAddress {
	modAddress, _ := sdk.AccAddressFromBech32(msg.ModeratorAddress)
	return []sdk.AccAddress{modAddress}
}
