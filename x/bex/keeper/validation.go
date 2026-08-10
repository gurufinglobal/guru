package keeper

import (
	"context"
	"math"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v10/modules/core/24-host"

	uint256decimal "github.com/gurufinglobal/guru/v3/internal/uint256"
	"github.com/gurufinglobal/guru/v3/x/bex/types"
)

const (
	maxUint256String     = uint256decimal.MaxDecimalString
	maxMetadataEntries   = 16
	maxMetadataKeyLen    = 64
	maxMetadataValLen    = 256
	minVolumeEpochSecs   = types.MinVolumeEpochSeconds
	maxVolumeEpochSecs   = types.MaxVolumeEpochSeconds
	minOracleStaleSecs   = uint32(1)
	maxOracleStaleSecs   = uint32(3600)
	requiredExchangePort = "transwap"
)

var maxUint256Int = uint256decimal.Max()

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

// validateRequiredIntString validates a decimal integer string for fields where empty value is invalid.
func validateRequiredIntString(name, value string) (sdkmath.Int, error) {
	if value == "" {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s is required", name)
	}
	amount, err := uint256decimal.ParseCanonical(value)
	if err != nil {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s must be a canonical uint256 decimal: %v", name, err)
	}
	return amount, nil
}

// validateExchangeLimitIntString validates exchange limit/cap values.
// For exchange limit/cap fields, empty string is intentionally interpreted as 0 (unlimited).
func validateExchangeLimitIntString(name, value string) (sdkmath.Int, error) {
	if value == "" {
		return sdkmath.ZeroInt(), nil
	}
	amount, err := uint256decimal.ParseCanonical(value)
	if err != nil {
		return sdkmath.Int{}, types.ErrInvalidRequest.Wrapf("%s must be empty or a canonical uint256 decimal: %v", name, err)
	}
	return amount, nil
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > maxMetadataEntries {
		return types.ErrInvalidRequest.Wrap("metadata has too many entries")
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := metadata[key]
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

func validateStatus(status types.ExchangeStatus) error {
	switch status {
	case types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE,
		types.ExchangeStatus_EXCHANGE_STATUS_DELETED:
		return nil
	default:
		return types.ErrInvalidRequest.Wrap("invalid exchange status")
	}
}

func validateMutableStatus(status types.ExchangeStatus) error {
	switch status {
	case types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		types.ExchangeStatus_EXCHANGE_STATUS_INACTIVE:
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

func validateMutableExchangeConfig(exchange *types.Exchange) error {
	if exchange == nil {
		return types.ErrInvalidRequest.Wrap("exchange cannot be nil")
	}
	if err := validateMutableStatus(exchange.GetStatus()); err != nil {
		return err
	}
	return validateExchangeConfig(exchange)
}

func validateExchangeConfig(exchange *types.Exchange) error {
	if exchange == nil {
		return types.ErrInvalidRequest.Wrap("exchange cannot be nil")
	}
	if exchange.GetId() == 0 {
		return types.ErrInvalidRequest.Wrap("exchange id cannot be zero")
	}
	if exchange.GetVolumeWindowGeneration() == 0 {
		return types.ErrInvalidRequest.Wrap("volume_window_generation must be non-zero")
	}
	if err := validateStatus(exchange.GetStatus()); err != nil {
		return err
	}
	if err := validateRoute(exchange.GetDenomA(), exchange.GetPortA(), exchange.GetChannelA()); err != nil {
		return err
	}
	if err := validateRoute(exchange.GetDenomB(), exchange.GetPortB(), exchange.GetChannelB()); err != nil {
		return err
	}
	if exchange.GetPortA() != requiredExchangePort || exchange.GetPortB() != requiredExchangePort {
		return types.ErrInvalidRoute.Wrap("BEX exchange routes must use transwap port")
	}
	ibcDenomA, err := buildIBCDenom(exchange.GetDenomA(), exchange.GetPortA(), exchange.GetChannelA())
	if err != nil {
		return err
	}
	ibcDenomB, err := buildIBCDenom(exchange.GetDenomB(), exchange.GetPortB(), exchange.GetChannelB())
	if err != nil {
		return err
	}
	seenDenoms := make(map[string]string, 4)
	for _, candidate := range []struct {
		name  string
		denom string
	}{
		{name: "denom_a", denom: strings.TrimSpace(exchange.GetDenomA())},
		{name: "denom_b", denom: strings.TrimSpace(exchange.GetDenomB())},
		{name: "ibc_denom_a", denom: ibcDenomA},
		{name: "ibc_denom_b", denom: ibcDenomB},
	} {
		if previous, found := seenDenoms[candidate.denom]; found {
			return types.ErrInvalidRoute.Wrapf("%s and %s must resolve to distinct denoms", previous, candidate.name)
		}
		seenDenoms[candidate.denom] = candidate.name
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
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "limit_a_to_b", value: exchange.GetLimitAToB()},
		{name: "limit_b_to_a", value: exchange.GetLimitBToA()},
		{name: "volume_cap_a_to_b", value: exchange.GetVolumeCapAToB()},
		{name: "volume_cap_b_to_a", value: exchange.GetVolumeCapBToA()},
	} {
		if _, err := validateExchangeLimitIntString(field.name, field.value); err != nil {
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
	if exchange.GetPendingVolumeEpochSeconds() != 0 && exchange.GetRevision() == ^uint64(0) {
		return types.ErrInvalidRequest.Wrap("pending volume epoch requires an available revision")
	}
	if exchange.GetPendingVolumeEpochSeconds() != 0 && exchange.GetVolumeWindowGeneration() == ^uint64(0) {
		return types.ErrInvalidRequest.Wrap("pending volume epoch requires an available volume window generation")
	}
	if exchange.GetPendingVolumeEpochEffectiveAtUnix() > uint64(math.MaxInt64) {
		return types.ErrInvalidRequest.Wrap("pending_volume_epoch_effective_at_unix exceeds supported Unix time")
	}
	if err := validateOracleStalenessSeconds(exchange.GetMaxOracleStalenessSeconds()); err != nil {
		return err
	}
	if err := validateMetadata(exchange.GetMetadata()); err != nil {
		return err
	}
	return nil
}

func (k Keeper) validateActiveRoutes(ctx context.Context, exchange *types.Exchange) error {
	if exchange.GetStatus() != types.ExchangeStatus_EXCHANGE_STATUS_ACTIVE {
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

func protoCoinsToSDK(coins sdk.Coins) (sdk.Coins, error) {
	out := make(sdk.Coins, 0, len(coins))
	for _, coin := range coins {
		if err := coin.Validate(); err != nil {
			return nil, types.ErrInvalidRequest.Wrapf("invalid coin: %v", err)
		}
		out = append(out, coin)
	}
	out = out.Sort()
	if !out.IsValid() || !out.IsAllPositive() {
		return nil, types.ErrInvalidRequest.Wrap("coins must be positive and valid")
	}
	return out, nil
}

func sdkCoinsToProto(coins sdk.Coins) sdk.Coins {
	return append(sdk.Coins(nil), coins...).Sort()
}

func coinsToLedger(coins sdk.Coins) *types.FeeLedger {
	return &types.FeeLedger{Coins: sdkCoinsToProto(coins)}
}

func ledgerToCoins(ledger *types.FeeLedger) (sdk.Coins, error) {
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
