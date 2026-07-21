package types

import (
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
)

// ValidateToken validates a token denomination and amount.
func ValidateToken(t *Token) error {
	if t == nil {
		return errorsmod.Wrap(ErrInvalidDenomForTransfer, "token cannot be nil")
	}

	if err := ValidateDenom(t.Denom); err != nil {
		return errorsmod.Wrap(err, "invalid token denom")
	}

	return validateAmount(t.Amount)
}

// TokenToCoin converts a Token to an sdk.Coin that can be materialized by the
// local bank module. Base denominations are intentionally not validated by
// ValidateDenom because a remote chain may use different denomination rules.
// Once the trace has been resolved to a local bank denomination, however, it
// must satisfy the local SDK rules before sdk.NewCoin is called (which would
// otherwise panic for an invalid denomination).
func TokenToCoin(t *Token) (sdk.Coin, error) {
	if t == nil {
		return sdk.Coin{}, errorsmod.Wrap(ErrInvalidDenomForTransfer, "token cannot be nil")
	}
	if err := ValidateDenom(t.Denom); err != nil {
		return sdk.Coin{}, errorsmod.Wrap(err, "invalid token denom")
	}

	transferAmount, err := parseAmount(t.Amount)
	if err != nil {
		return sdk.Coin{}, err
	}

	localDenom := DenomIBCDenom(t.Denom)
	if err := sdk.ValidateDenom(localDenom); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(
			ErrInvalidDenomForTransfer,
			"denomination %q cannot be materialized as a local bank coin: %v",
			localDenom,
			err,
		)
	}

	return sdk.NewCoin(localDenom, transferAmount), nil
}

// UnboundedSpendLimit returns the sentinel value for unlimited spend grants.
func UnboundedSpendLimit() sdkmath.Int {
	return uint256decimal.Max()
}

func validateAmount(raw string) error {
	_, err := parseAmount(raw)
	return err
}

func parseAmount(raw string) (sdkmath.Int, error) {
	amount, err := uint256decimal.ParseCanonicalPositive(raw)
	if err != nil {
		return sdkmath.Int{}, errorsmod.Wrapf(
			ErrInvalidAmount,
			"amount must be a canonical positive uint256 decimal: %q: %v",
			raw,
			err,
		)
	}
	return amount, nil
}
