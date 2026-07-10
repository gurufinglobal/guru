package types

import (
	"math/big"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	transwapv1 "github.com/gurufinglobal/guru/v3/api/guru/transwap/v1"
)

var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// ValidateToken validates a token denomination and amount.
func ValidateToken(t transwapv1.Token) error {
	if t.Denom == nil {
		return errorsmod.Wrap(ErrInvalidDenomForTransfer, "token denom cannot be nil")
	}

	if err := ValidateDenom(t.Denom); err != nil {
		return errorsmod.Wrap(err, "invalid token denom")
	}

	return validateAmount(t.Amount)
}

// TokenToCoin converts a Token to an sdk.Coin.
func TokenToCoin(t transwapv1.Token) (sdk.Coin, error) {
	if err := ValidateToken(t); err != nil {
		return sdk.Coin{}, err
	}

	transferAmount, ok := sdkmath.NewIntFromString(t.Amount)
	if !ok {
		return sdk.Coin{}, errorsmod.Wrapf(ErrInvalidAmount, "unable to parse transfer amount (%s) into math.Int", transferAmount)
	}

	return sdk.NewCoin(DenomIBCDenom(t.Denom), transferAmount), nil
}

// UnboundedSpendLimit returns the sentinel value for unlimited spend grants.
func UnboundedSpendLimit() sdkmath.Int {
	return sdkmath.NewIntFromBigInt(maxUint256)
}

func validateAmount(raw string) error {
	amount, ok := sdkmath.NewIntFromString(raw)
	if !ok {
		return errorsmod.Wrapf(ErrInvalidAmount, "unable to parse transfer amount (%s) into math.Int", raw)
	}

	if !amount.IsPositive() {
		return errorsmod.Wrapf(ErrInvalidAmount, "amount must be strictly positive: got %d", amount)
	}

	if amount.BigInt().Cmp(maxUint256) > 0 {
		return errorsmod.Wrapf(ErrInvalidAmount, "amount exceeds uint256 maximum: got %s", raw)
	}

	return nil
}
