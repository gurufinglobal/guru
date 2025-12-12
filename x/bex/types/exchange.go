package types

import (
	"encoding/json"
	fmt "fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Validate validates the exchange
func (e *Exchange) Validate() error {
	// validate id
	err := ValidateExchangeID(e.Id)
	if err != nil {
		return err
	}

	// validate admin address
	_, err = sdk.AccAddressFromBech32(e.AdminAddress)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " admin_address: %s", err)
	}

	// validate reserve address
	_, err = sdk.AccAddressFromBech32(e.ReserveAddress)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, " reserve_address: %s", err)
	}

	// validate denoms
	if err := sdk.ValidateDenom(e.DenomA); err != nil {
		return errorsmod.Wrapf(ErrInvalidDenom, " denom_a")
	}
	if err := sdk.ValidateDenom(e.DenomB); err != nil {
		return errorsmod.Wrapf(ErrInvalidDenom, " denom_b")
	}
	if err := sdk.ValidateDenom(e.IbcDenomA); err != nil {
		return errorsmod.Wrapf(ErrInvalidDenom, " ibc_denom_a")
	}
	if err := sdk.ValidateDenom(e.IbcDenomB); err != nil {
		return errorsmod.Wrapf(ErrInvalidDenom, " ibc_denom_b")
	}

	// validate ports
	if e.PortA == "" {
		return errorsmod.Wrapf(ErrInvalidPort, " port_a")
	}
	if e.PortB == "" {
		return errorsmod.Wrapf(ErrInvalidPort, " port_b")
	}

	// validate channels
	if e.ChannelA == "" {
		return errorsmod.Wrapf(ErrInvalidChannel, " channel_a")
	}
	if e.ChannelB == "" {
		return errorsmod.Wrapf(ErrInvalidChannel, " channel_b")
	}

	// validate fee
	if err := ValidateExchangeFee(e.Fee); err != nil {
		return err
	}

	// validate limit
	if !e.Limit.IsNil() && e.Limit.IsNegative() {
		return errorsmod.Wrapf(ErrInvalidLimit, " limit is negative")
	}

	// validate status
	return ValidateExchangeStatus(e.Status)
}

// ValidateExchangeID validates the exchange id
func ValidateExchangeID(id math.Int) error {
	if id.IsNil() {
		return errorsmod.Wrapf(ErrInvalidID, " exchange id is nil")
	}
	if id.IsZero() {
		return errorsmod.Wrapf(ErrInvalidID, " exchange id is zero")
	}
	return nil
}

// ValidateExchangeKey validates the exchange key
func ValidateExchangeUpdateKeys(key string) error {
	if key == ExchangeKeyAdminAddress || key == ExchangeKeyReserveAddress || key == ExchangeKeyFee || key == ExchangeKeyLimit || key == ExchangeKeyStatus || key == ExchangeKeyMetadata {
		return nil
	}
	return errorsmod.Wrapf(ErrInvalidKey, " key cannot be updated or does not exist")
}

func ValidateExchangeStatus(status string) error {
	if status != ExchangeStatusActive && status != ExchangeStatusInactive {
		return errorsmod.Wrapf(ErrInvalidStatus, " status is invalid. accepted statuses: %s, %s", ExchangeStatusActive, ExchangeStatusInactive)
	}
	return nil
}

func ValidateExchangeFee(fee math.LegacyDec) error {
	if fee.IsNil() {
		return errorsmod.Wrapf(ErrInvalidFee, " fee is nil")
	}
	if fee.IsNegative() {
		return errorsmod.Wrapf(ErrInvalidFee, " fee is negative")
	}
	return nil
}

func ValidateExchangeLimit(limit math.LegacyDec) error {
	if limit.IsNil() {
		return errorsmod.Wrapf(ErrInvalidLimit, " limit is nil")
	}
	if limit.IsNegative() {
		return errorsmod.Wrapf(ErrInvalidLimit, " limit is negative")
	}
	return nil
}

func ValidateAndUnmarshalExchangeMetedataFromStr(metadata string) (map[string]string, error) {
	metadataMap := make(map[string]string)
	err := json.Unmarshal([]byte(metadata), &metadataMap)
	if err != nil {
		return nil, errorsmod.Wrapf(ErrInvalidMetadata, " %s. Expected format: {\"key\":\"value\"}", err)
	}
	for key, value := range metadataMap {
		if key == "" {
			return nil, errorsmod.Wrapf(ErrInvalidMetadata, " key is empty")
		}
		if value == "" {
			return nil, errorsmod.Wrapf(ErrInvalidMetadata, " value is empty")
		}
	}
	return metadataMap, nil
}

func (e *Exchange) IsSupportedToken(denom string) bool {
	return e.DenomA == denom || e.DenomB == denom
}

func (e *Exchange) GetSwapData(denom string) (string, string, string, error) {
	if denom == e.DenomA {
		return e.ChannelB, e.PortB, e.IbcDenomB, nil
	}
	if denom == e.DenomB {
		return e.ChannelA, e.PortA, e.IbcDenomA, nil
	}
	return "", "", "", errorsmod.Wrapf(ErrInvalidDenom, "denom is not supported: %s", denom)
}

func (e *Exchange) GetSwapDataWithRate(denom string, rate math.LegacyDec) (string, string, string, math.LegacyDec, error) {
	if denom == e.DenomA {
		return e.ChannelB, e.PortB, e.IbcDenomB, rate, nil
	}
	if denom == e.DenomB {
		if rate.IsZero() {
			return "", "", "", math.LegacyDec{}, errorsmod.Wrap(ErrInvalidRate, "rate cannot be zero")
		}
		return e.ChannelA, e.PortA, e.IbcDenomA, math.LegacyOneDec().Quo(rate), nil
	}
	return "", "", "", math.LegacyDec{}, errorsmod.Wrapf(ErrInvalidDenom, "denom is not supported: %s", denom)
}

func (e *Exchange) GetOppositeSwapDataWithRate(denom string, rate math.LegacyDec) (string, string, string, math.LegacyDec, error) {
	if denom == e.DenomA {
		return e.GetSwapDataWithRate(e.DenomB, rate)
	}
	if denom == e.DenomB {
		return e.GetSwapDataWithRate(e.DenomA, rate)
	}
	return "", "", "", math.LegacyDec{}, errorsmod.Wrapf(ErrInvalidDenom, "denom is not supported: %s", denom)
}

func (e *Exchange) ToString() string {
	return fmt.Sprintf("admin_address: %s, reserve_address: %s, fee: %s, limit: %s, status: %s, metadata: %s", e.AdminAddress, e.ReserveAddress, e.Fee, e.Limit, e.Status, e.Metadata)
}
