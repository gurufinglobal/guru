package keeper

import (
	"bytes"
	"context"
	"strings"

	"cosmossdk.io/core/address"
	"cosmossdk.io/log"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
)

type Keeper struct {
	authority    sdk.AccAddress
	accountCodec address.Codec
	bankKeeper   BankKeeper
	feeMarket    FeeMarketKeeper

	params              collections.Item[constitutiontypes.Params]
	baseAddress         collections.Item[string]
	moderatorAddress    collections.Item[string]
	separationRatio     collections.Item[constitutiontypes.SeparationRatio]
	minGasPriceSchedule collections.Item[constitutiontypes.MinGasPriceSchedule]

	schema collections.Schema
}

func NewKeeper(
	authority sdk.AccAddress,
	storeService store.KVStoreService,
	cdc codec.Codec,
	accountCodec address.Codec,
	bankKeeper BankKeeper,
) Keeper {
	k := Keeper{
		authority:    authority,
		accountCodec: accountCodec,
		bankKeeper:   bankKeeper,
	}

	sb := collections.NewSchemaBuilder(storeService)

	k.params = collections.NewItem(sb, constitutiontypes.ParamsKey, "params", codec.CollValue[constitutiontypes.Params](cdc))
	k.baseAddress = collections.NewItem(sb, constitutiontypes.BaseAddressKey, "base_address", collections.StringValue)
	k.moderatorAddress = collections.NewItem(sb, constitutiontypes.ModeratorAddressKey, "moderator_address", collections.StringValue)
	k.separationRatio = collections.NewItem(sb, constitutiontypes.SeparationRatioKey, "separation_ratio", codec.CollValue[constitutiontypes.SeparationRatio](cdc))
	k.minGasPriceSchedule = collections.NewItem(sb, constitutiontypes.MinGasPriceKey, "min_gas_price_schedule", codec.CollValue[constitutiontypes.MinGasPriceSchedule](cdc))
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.schema = schema

	return k
}

func (k *Keeper) SetFeeMarketKeeper(feeMarket FeeMarketKeeper) {
	k.feeMarket = feeMarket
}

func (k Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+constitutiontypes.ModuleName)
}

func (k Keeper) AuthorityAddressString() (string, error) {
	return k.accountCodec.BytesToString(k.authority)
}

func (k Keeper) GetBaseAddress(ctx context.Context) (string, error) {
	return k.baseAddress.Get(ctx)
}

func (k Keeper) SetBaseAddress(ctx context.Context, baseAddress string) error {
	if err := k.ValidateBaseAddress(baseAddress); err != nil {
		return err
	}

	return k.baseAddress.Set(ctx, baseAddress)
}

func (k Keeper) UpdateBaseAddress(ctx context.Context, baseAddress string) error {
	return k.SetBaseAddress(ctx, baseAddress)
}

func (k Keeper) ValidateBaseAddress(baseAddress string) error {
	return k.validateAddress("base_address", baseAddress, true)
}

func (k Keeper) GetModeratorAddress(ctx context.Context) (string, error) {
	return k.moderatorAddress.Get(ctx)
}

func (k Keeper) SetModeratorAddress(ctx context.Context, moderatorAddress string) error {
	if err := k.ValidateModeratorAddress(moderatorAddress); err != nil {
		return err
	}

	return k.moderatorAddress.Set(ctx, moderatorAddress)
}

func (k Keeper) UpdateModeratorAddress(ctx context.Context, moderatorAddress string) error {
	return k.SetModeratorAddress(ctx, moderatorAddress)
}

func (k Keeper) ValidateModeratorAddress(moderatorAddress string) error {
	return k.validateAddress("moderator_address", moderatorAddress, false)
}

func (k Keeper) validateAddress(fieldName, value string, rejectBlocked bool) error {
	if strings.TrimSpace(value) == "" {
		return constitutiontypes.ErrInvalidParams.Wrapf("%s cannot be empty", fieldName)
	}

	addressBytes, err := k.accountCodec.StringToBytes(value)
	if err != nil {
		return constitutiontypes.ErrInvalidParams.Wrapf("invalid %s: %v", fieldName, err)
	}
	address := sdk.AccAddress(addressBytes)

	if bytes.Equal(address, k.authority) {
		return constitutiontypes.ErrInvalidParams.Wrapf("%s must be explicitly configured and cannot equal authority", fieldName)
	}
	if rejectBlocked && k.bankKeeper != nil {
		blocked := k.bankKeeper.BlockedAddr(address)
		if !blocked {
			// The SDK bank keeper keys blocked addresses by address string. Use
			// the injected codec too, so this check is not coupled to global SDK
			// Bech32 configuration.
			canonical, err := k.accountCodec.BytesToString(address)
			if err != nil {
				return constitutiontypes.ErrInvalidParams.Wrapf("invalid %s: %v", fieldName, err)
			}
			blocked = k.bankKeeper.GetBlockedAddresses()[canonical]
		}
		if blocked {
			return constitutiontypes.ErrInvalidParams.Wrapf("%s cannot be a blocked address", fieldName)
		}
	}

	return nil
}
