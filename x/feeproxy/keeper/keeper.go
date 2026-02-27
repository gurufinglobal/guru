package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	corestore "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/x/feeproxy/types"
)

// Keeper wraps an existing ICS20 TransferKeeper and intercepts only the
// forwarded-transfer path (PFM -> Transfer(msg)) to charge protocol fees.
//
// Fee policy parameters are owned by x/feeproxy and persisted via collections.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService

	originalKeeper types.TransferKeeper
	bankKeeper     types.BankKeeper
	authority      string

	Schema               collections.Schema
	ModeratorAddressItem collections.Item[string]
	ParamsItem           collections.Item[types.Params]
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	originalKeeper types.TransferKeeper,
	bankKeeper types.BankKeeper,
	authority string,
) Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	stringValue := collcodec.KeyToValueCodec(collcodec.NewStringKeyCodec[string]())

	k := Keeper{
		cdc:                  cdc,
		storeService:         storeService,
		originalKeeper:       originalKeeper,
		bankKeeper:           bankKeeper,
		authority:            authority,
		ModeratorAddressItem: collections.NewItem(sb, types.ModeratorAddressKeyPrefix, "moderator_address", stringValue),
		ParamsItem:           collections.NewItem(sb, types.ParamsKeyPrefix, "params", codec.CollValue[types.Params, *types.Params](cdc)),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

func (k Keeper) GetAuthority() string {
	return k.authority
}

// BankKeeper exposes the bank keeper dependency for middleware operations
// (settlement/refund at ack/timeout time).
func (k Keeper) BankKeeper() types.BankKeeper {
	return k.bankKeeper
}

func (k Keeper) GetModeratorAddress(ctx context.Context) (string, error) {
	addr, err := k.ModeratorAddressItem.Get(ctx)
	switch {
	case err == nil:
		return addr, nil
	case errors.Is(err, collections.ErrNotFound):
		// fallback: keep the module operable even if InitGenesis didn't run yet
		// (should not happen). We default to the keeper authority.
		return k.authority, nil
	default:
		return "", err
	}
}

func (k Keeper) SetModeratorAddress(ctx context.Context, moderatorAddr string) error {
	if moderatorAddr == "" {
		return fmt.Errorf("invalid moderator_address: empty address string is not allowed")
	}
	if _, err := sdk.AccAddressFromBech32(moderatorAddr); err != nil {
		return fmt.Errorf("invalid moderator_address: %w", err)
	}
	return k.ModeratorAddressItem.Set(ctx, moderatorAddr)
}

func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	p, err := k.ParamsItem.Get(ctx)
	switch {
	case err == nil:
		return p, nil
	case errors.Is(err, collections.ErrNotFound):
		// fallback: keep the module operable even if InitGenesis didn't run yet
		// (should not happen). Default admin/reserve to moderator/authority and fee to 0.
		mod, modErr := k.GetModeratorAddress(ctx)
		if modErr != nil {
			return types.Params{}, modErr
		}
		return types.Params{
			AdminAddress:   mod,
			ReserveAddress: mod,
			FeePercentage:  types.DefaultParams().FeePercentage,
		}, nil
	default:
		return types.Params{}, err
	}
}

func (k Keeper) SetParams(ctx context.Context, p types.Params) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return k.ParamsItem.Set(ctx, p)
}
