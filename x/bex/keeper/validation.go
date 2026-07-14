package keeper

import (
	"context"
	"math/big"
	"sort"
	"strings"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v11/modules/core/24-host"
	bexv1 "github.com/gurufinglobal/guru/v3/api/guru/bex/v1"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const (
	maxUint256String   = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	maxMetadataEntries = 16
	maxMetadataKeyLen  = 64
	maxMetadataValLen  = 256
	maxIntDigits       = 78
	minVolumeEpochSecs = uint32(86400)
	maxVolumeEpochSecs = uint32(604800)
	minOracleStaleSecs = uint32(1)
	maxOracleStaleSecs = uint32(3600)
)

var maxUint256Int = func() sdkmath.Int {
	amount, ok := sdkmath.NewIntFromString(maxUint256String)
	if !ok {
		panic("invalid max uint256")
	}
	return amount
}()

func (k Keeper) canonicalAddress(address string) (string, sdk.AccAddress, error) {
	bz, err := k.accountCodec.StringToBytes(strings.TrimSpace(address))
	if err != nil {
		return "", nil, err
	}
	canonical, err := k.accountCodec.BytesToString(bz)
	if err != nil {
		return "", nil, err
	}
	return canonical, sdk.AccAddress(bz), nil
}

func validateFeeBps(bps uint32) error {
	if bps >= 10000 {
		return types.ErrInvalidFeeBps.Wrap("fee bps must be less than 10000")
	}
	return nil
}

func validateIntString(name, value string) (sdkmath.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "0"
	}
	digits, negative, decimal := decimalDigits(value)
	if !decimal {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s must be a decimal integer string", name)
	}
	if len(digits) > maxIntDigits {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s exceeds %d decimal digits", name, maxIntDigits)
	}
	if negative {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s cannot be negative", name)
	}
	normalizedDigits := strings.TrimLeft(digits, "0")
	if normalizedDigits == "" {
		normalizedDigits = "0"
	}
	if len(normalizedDigits) == maxIntDigits && normalizedDigits > maxUint256String {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s exceeds uint256 max", name)
	}
	amount, ok := new(big.Int).SetString(normalizedDigits, 10)
	if !ok {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s must be a decimal integer string", name)
	}
	return sdkmath.NewIntFromBigInt(amount), nil
}

// validateRequiredIntString validates a decimal integer string for fields where empty value is invalid.
func validateRequiredIntString(name, value string) (sdkmath.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s is required", name)
	}
	return validateIntString(name, value)
}

// validateExchangeLimitIntString validates exchange limit/cap values.
// For exchange limit/cap fields, empty string is intentionally interpreted as 0 (unlimited).
func validateExchangeLimitIntString(name, value string) (sdkmath.Int, error) {
	return validateIntString(name, value)
}

func decimalDigits(value string) (string, bool, bool) {
	if value == "" {
		return "", false, false
	}
	negative := false
	if value[0] == '-' || value[0] == '+' {
		negative = value[0] == '-'
		value = value[1:]
	}
	if value == "" {
		return "", negative, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return value, negative, false
		}
	}
	return value, negative, true
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > maxMetadataEntries {
		return types.ErrInvalidRequest.Wrap("metadata has too many entries")
	}
	for key, value := range metadata {
		if key == "" {
			return types.ErrInvalidRequest.Wrap("metadata key cannot be empty")
		}
		if len([]byte(key)) > maxMetadataKeyLen {
			return types.ErrInvalidRequest.Wrapf("metadata key %q too long", key)
		}
		if len([]byte(value)) > maxMetadataValLen {
			return types.ErrInvalidRequest.Wrapf("metadata value for %q too long", key)
		}
	}
	return nil
}

func validateRoute(denom, portID, channelID string) error {
	if err := sdk.ValidateDenom(strings.TrimSpace(denom)); err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid denom %q: %v", denom, err)
	}
	if err := host.PortIdentifierValidator(strings.TrimSpace(portID)); err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid port %q: %v", portID, err)
	}
	if err := host.ChannelIdentifierValidator(strings.TrimSpace(channelID)); err != nil {
		return types.ErrInvalidRoute.Wrapf("invalid channel %q: %v", channelID, err)
	}
	return nil
}

func buildIBCDenom(denom, portID, channelID string) (string, error) {
	if err := validateRoute(denom, portID, channelID); err != nil {
		return "", err
	}
	ibcDenom := transfertypes.NewDenom(strings.TrimSpace(denom), transfertypes.NewHop(strings.TrimSpace(portID), strings.TrimSpace(channelID)))
	return ibcDenom.IBCDenom(), nil
}

func validateStatus(status bexv1.ExchangeStatus) error {
	switch status {
	case bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
		bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED:
		return nil
	default:
		return types.ErrInvalidRequest.Wrap("invalid exchange status")
	}
}

func validateMutableStatus(status bexv1.ExchangeStatus) error {
	switch status {
	case bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		bexv1.ExchangeStatus_EXCHANGE_STATUS_INACTIVE:
		return nil
	default:
		return types.ErrInvalidRequest.Wrap("register/update status must be active or inactive")
	}
}

func validateVolumeEpochSeconds(name string, seconds uint32, allowZero bool) error {
	if seconds == 0 && allowZero {
		return nil
	}
	if seconds < minVolumeEpochSecs || seconds > maxVolumeEpochSecs {
		return types.ErrInvalidRequest.Wrapf("%s must be between %d and %d seconds", name, minVolumeEpochSecs, maxVolumeEpochSecs)
	}
	return nil
}

func validateOracleStalenessSeconds(seconds uint32) error {
	if seconds < minOracleStaleSecs || seconds > maxOracleStaleSecs {
		return types.ErrInvalidRequest.Wrapf("max_oracle_staleness_seconds must be between %d and %d seconds", minOracleStaleSecs, maxOracleStaleSecs)
	}
	return nil
}

func validateMutableExchangeConfig(exchange *bexv1.Exchange) error {
	if exchange == nil {
		return types.ErrInvalidRequest.Wrap("exchange cannot be nil")
	}
	if err := validateMutableStatus(exchange.GetStatus()); err != nil {
		return err
	}
	return validateExchangeConfig(exchange)
}

func validateExchangeConfig(exchange *bexv1.Exchange) error {
	if exchange == nil {
		return types.ErrInvalidRequest.Wrap("exchange cannot be nil")
	}
	if exchange.GetId() == 0 {
		return types.ErrInvalidRequest.Wrap("exchange id cannot be zero")
	}
	if err := validateStatus(exchange.GetStatus()); err != nil {
		return err
	}
	if exchange.GetStatus() == bexv1.ExchangeStatus_EXCHANGE_STATUS_DELETED {
		return nil
	}
	if err := validateRoute(exchange.GetDenomA(), exchange.GetPortA(), exchange.GetChannelA()); err != nil {
		return err
	}
	if err := validateRoute(exchange.GetDenomB(), exchange.GetPortB(), exchange.GetChannelB()); err != nil {
		return err
	}
	if strings.TrimSpace(exchange.GetDenomA()) == strings.TrimSpace(exchange.GetDenomB()) {
		return types.ErrInvalidRoute.Wrap("denom_a and denom_b must be distinct")
	}
	if strings.TrimSpace(exchange.GetOracleSymbolAToB()) == "" || strings.TrimSpace(exchange.GetOracleSymbolBToA()) == "" {
		return types.ErrInvalidOracleRate.Wrap("oracle symbols cannot be empty")
	}
	if err := validateFeeBps(exchange.GetFeeBpsAToB()); err != nil {
		return err
	}
	if err := validateFeeBps(exchange.GetFeeBpsBToA()); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"limit_a_to_b":      exchange.GetLimitAToB(),
		"limit_b_to_a":      exchange.GetLimitBToA(),
		"volume_cap_a_to_b": exchange.GetVolumeCapAToB(),
		"volume_cap_b_to_a": exchange.GetVolumeCapBToA(),
	} {
		if _, err := validateExchangeLimitIntString(name, value); err != nil {
			return err
		}
	}
	if err := validateVolumeEpochSeconds("volume_epoch_seconds", exchange.GetVolumeEpochSeconds(), false); err != nil {
		return err
	}
	if err := validateVolumeEpochSeconds("pending_volume_epoch_seconds", exchange.GetPendingVolumeEpochSeconds(), true); err != nil {
		return err
	}
	if exchange.GetPendingVolumeEpochSeconds() == 0 && exchange.GetPendingVolumeEpochEffectiveAtUnix() != 0 {
		return types.ErrInvalidRequest.Wrap("pending_volume_epoch_effective_at_unix requires pending_volume_epoch_seconds")
	}
	if exchange.GetPendingVolumeEpochSeconds() != 0 && exchange.GetPendingVolumeEpochEffectiveAtUnix() == 0 {
		return types.ErrInvalidRequest.Wrap("pending_volume_epoch_seconds requires pending_volume_epoch_effective_at_unix")
	}
	if err := validateOracleStalenessSeconds(exchange.GetMaxOracleStalenessSeconds()); err != nil {
		return err
	}
	if err := validateMetadata(exchange.GetMetadata()); err != nil {
		return err
	}
	return nil
}

func (k Keeper) validateActiveRoutes(ctx context.Context, exchange *bexv1.Exchange) error {
	if exchange.GetStatus() != bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
		return nil
	}
	if k.channelKeeper == nil {
		return types.ErrInvalidRoute.Wrap("channel keeper is required for active exchange route validation")
	}
	if err := k.validateOpenChannel(ctx, exchange.GetPortA(), exchange.GetChannelA()); err != nil {
		return err
	}
	return k.validateOpenChannel(ctx, exchange.GetPortB(), exchange.GetChannelB())
}

func (k Keeper) validateOpenChannel(ctx context.Context, portID, channelID string) error {
	portID = strings.TrimSpace(portID)
	channelID = strings.TrimSpace(channelID)
	channel, found := k.channelKeeper.GetChannel(sdk.UnwrapSDKContext(ctx), portID, channelID)
	if !found {
		return types.ErrInvalidRoute.Wrapf("channel %s/%s not found", portID, channelID)
	}
	if channel.State != channeltypes.OPEN {
		return types.ErrInvalidRoute.Wrapf("channel %s/%s is not open", portID, channelID)
	}
	return nil
}

func protoCoinsToSDK(coins []*basev1beta1.Coin) (sdk.Coins, error) {
	out := make(sdk.Coins, 0, len(coins))
	for _, coin := range coins {
		if coin == nil {
			return nil, types.ErrInvalidRequest.Wrap("coin cannot be nil")
		}
		amount, err := validateRequiredIntString("coin amount", coin.GetAmount())
		if err != nil {
			return nil, types.ErrInvalidRequest.Wrapf("invalid coin amount %q: %v", coin.GetAmount(), err)
		}
		sdkCoin := sdk.Coin{Denom: coin.GetDenom(), Amount: amount}
		if err := sdkCoin.Validate(); err != nil {
			return nil, types.ErrInvalidRequest.Wrapf("invalid coin: %v", err)
		}
		out = append(out, sdkCoin)
	}
	out = out.Sort()
	if !out.IsValid() || !out.IsAllPositive() {
		return nil, types.ErrInvalidRequest.Wrap("coins must be positive and valid")
	}
	return out, nil
}

func sdkCoinsToProto(coins sdk.Coins) []*basev1beta1.Coin {
	coins = coins.Sort()
	out := make([]*basev1beta1.Coin, 0, len(coins))
	for _, coin := range coins {
		out = append(out, &basev1beta1.Coin{Denom: coin.Denom, Amount: coin.Amount.String()})
	}
	return out
}

func coinsToLedger(coins sdk.Coins) *bexv1.FeeLedger {
	return &bexv1.FeeLedger{Coins: sdkCoinsToProto(coins)}
}

func ledgerToCoins(ledger *bexv1.FeeLedger) (sdk.Coins, error) {
	if ledger == nil {
		return sdk.Coins{}, nil
	}
	coins, err := protoCoinsToSDK(ledger.GetCoins())
	if err != nil {
		if len(ledger.GetCoins()) == 0 {
			return sdk.Coins{}, nil
		}
		return nil, err
	}
	return coins, nil
}

func sortedMetadataCopy(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(in))
	for _, key := range keys {
		out[key] = in[key]
	}
	return out
}
