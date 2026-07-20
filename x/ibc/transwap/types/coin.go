package types

import (
	"fmt"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
)

// SDKCoinToProto converts an SDK coin into the API coin type emitted by
// protoc-gen-go-pulsar.
func SDKCoinToProto(coin sdk.Coin) *basev1beta1.Coin {
	return &basev1beta1.Coin{
		Denom:  coin.Denom,
		Amount: coin.Amount.String(),
	}
}

// SDKCoinsToProto converts SDK coins into API coins emitted by
// protoc-gen-go-pulsar.
func SDKCoinsToProto(coins sdk.Coins) []*basev1beta1.Coin {
	out := make([]*basev1beta1.Coin, 0, len(coins))
	for _, coin := range coins {
		out = append(out, SDKCoinToProto(coin))
	}
	return out
}

// ProtoCoinToSDK converts a Pulsar/API coin into an SDK coin.
func ProtoCoinToSDK(coin *basev1beta1.Coin) (sdk.Coin, error) {
	if coin == nil {
		return sdk.Coin{Amount: sdkmath.ZeroInt()}, nil
	}
	if err := sdk.ValidateDenom(coin.Denom); err != nil {
		return sdk.Coin{}, err
	}

	amount, err := uint256decimal.ParseCanonical(coin.Amount)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin amount %q for denom %s", coin.Amount, coin.Denom)
	}

	return sdk.NewCoin(coin.Denom, amount), nil
}

// ProtoCoinsToSDK converts Pulsar/API coins into SDK coins.
func ProtoCoinsToSDK(coins []*basev1beta1.Coin) (sdk.Coins, error) {
	out := make(sdk.Coins, 0, len(coins))
	for i, coin := range coins {
		if coin == nil {
			return nil, fmt.Errorf("coin %d cannot be nil", i)
		}

		sdkCoin, err := ProtoCoinToSDK(coin)
		if err != nil {
			return nil, fmt.Errorf("invalid coin %d: %w", i, err)
		}
		out = append(out, sdkCoin)
	}

	if err := out.Validate(); err != nil {
		return nil, err
	}

	return out.Sort(), nil
}
