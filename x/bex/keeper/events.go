package keeper

import (
	"context"
	"strconv"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

func emitEvent(ctx context.Context, eventType string, attrs ...sdk.Attribute) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(eventType, attrs...))
}

func exchangeIDAttr(exchangeID uint64) sdk.Attribute {
	return sdk.NewAttribute(types.AttributeKeyExchangeID, strconv.FormatUint(exchangeID, 10))
}

func uint64Attr(key string, value uint64) sdk.Attribute {
	return sdk.NewAttribute(key, strconv.FormatUint(value, 10))
}

func intAttr(key string, value sdkmath.Int) sdk.Attribute {
	return sdk.NewAttribute(key, value.String())
}

func directionAttr(direction types.SwapDirection) sdk.Attribute {
	return sdk.NewAttribute(types.AttributeKeyDirection, direction.String())
}
