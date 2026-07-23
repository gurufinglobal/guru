package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
)

// SDKCoinToProto preserves the historical helper boundary while internal gogo
// protobuf messages now embed sdk.Coin directly.
func SDKCoinToProto(coin sdk.Coin) sdk.Coin {
	return coin
}

// SDKCoinsToProto returns a slice copy suitable for an internal gogo genesis
// message. Coin amounts are immutable values.
func SDKCoinsToProto(coins sdk.Coins) sdk.Coins {
	return append(sdk.Coins(nil), coins...)
}

// ProtoCoinToSDK validates an sdk.Coin decoded through the internal gogo
// protobuf boundary. In particular, it rejects an absent non-nullable coin,
// whose amount is the uninitialized math.Int zero value.
func ProtoCoinToSDK(coin sdk.Coin) (sdk.Coin, error) {
	if err := coin.Validate(); err != nil {
		return sdk.Coin{}, err
	}

	amount, err := uint256decimal.ParseCanonical(coin.Amount.String())
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin amount %q for denom %s", coin.Amount.String(), coin.Denom)
	}

	return sdk.NewCoin(coin.Denom, amount), nil
}

// ProtoCoinsToSDK validates coins decoded through the internal gogo protobuf
// boundary and returns a sorted copy.
func ProtoCoinsToSDK(coins sdk.Coins) (sdk.Coins, error) {
	out := make(sdk.Coins, 0, len(coins))
	for i, coin := range coins {
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
