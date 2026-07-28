package types

import sdk "github.com/cosmos/cosmos-sdk/types"

func NewMsgChangeModerator(moderator, newModerator sdk.AccAddress) *MsgChangeModerator {
	return &MsgChangeModerator{ModeratorAddress: moderator.String(), NewModeratorAddress: newModerator.String()}
}

func NewMsgRegisterDiscounts(moderator sdk.AccAddress, discounts []AccountDiscount) *MsgRegisterDiscounts {
	return &MsgRegisterDiscounts{ModeratorAddress: moderator.String(), Discounts: discounts}
}

func NewMsgRemoveDiscounts(moderator, address sdk.AccAddress, module, msgType string) *MsgRemoveDiscounts {
	return &MsgRemoveDiscounts{ModeratorAddress: moderator.String(), Address: address.String(), Module: module, MsgType: msgType}
}
