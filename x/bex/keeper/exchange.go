package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"cosmossdk.io/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/gurufinglobal/guru/v2/x/bex/types"
)

func (k Keeper) GetExchange(ctx sdk.Context, id math.Int) (*types.Exchange, error) {
	store := ctx.KVStore(k.storeKey)
	exchangeStore := prefix.NewStore(store, types.KeyExchanges)

	idBytes, err := id.Marshal()
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidID, "unable to marshal exchange id %v", err)
	}

	bz := exchangeStore.Get(idBytes)
	if bz == nil {
		return nil, nil
	}

	var exchange types.Exchange
	k.cdc.MustUnmarshal(bz, &exchange)

	return &exchange, nil
}

func (k Keeper) GetPaginatedExchanges(ctx sdk.Context, pagination *query.PageRequest) ([]types.Exchange, *query.PageResponse, error) {
	store := ctx.KVStore(k.storeKey)
	exchangeStore := prefix.NewStore(store, types.KeyExchanges)

	exchanges := []types.Exchange{}

	pageRes, err := query.Paginate(exchangeStore, pagination, func(key, value []byte) error {
		var exchange types.Exchange
		k.cdc.MustUnmarshal(value, &exchange)

		// add the exchange to the list
		exchanges = append(exchanges, exchange)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return exchanges, pageRes, nil
}

func (k Keeper) GetExchangesByOracleRequestID(ctx sdk.Context, oracleRequestID uint64) ([]types.Exchange, error) {
	allExchanges, _, err := k.GetPaginatedExchanges(ctx, &query.PageRequest{Limit: query.PaginationMaxLimit})
	if err != nil {
		return nil, err
	}

	exchanges := []types.Exchange{}
	for _, exchange := range allExchanges {
		if exchange.OracleRequestId == oracleRequestID {
			exchanges = append(exchanges, exchange)
		}
	}
	return exchanges, nil
}

func (k Keeper) SetExchange(ctx sdk.Context, exchange *types.Exchange) error {
	store := ctx.KVStore(k.storeKey)
	exchangeStore := prefix.NewStore(store, types.KeyExchanges)

	idBytes, err := exchange.Id.Marshal()
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidID, "unable to marshal exchange id %v", err)
	}

	// check if the exchange already exists
	bz := exchangeStore.Get(idBytes)
	if bz == nil {

		// check the next exchange id
		nextID, err := k.GetNextExchangeID(ctx)
		if err != nil {
			return err
		}
		if !nextID.Equal(exchange.Id) {
			return errorsmod.Wrapf(types.ErrInvalidID, "expected: %v, got: %v", nextID, exchange.Id)
		}

		// increment the next exchange id
		err = k.IncrementNextExchangeID(ctx)
		if err != nil {
			return err
		}
	}

	exchangeBytes := k.cdc.MustMarshal(exchange)
	exchangeStore.Set(idBytes, exchangeBytes)

	return nil
}

func (k Keeper) GetNextExchangeID(ctx sdk.Context) (math.Int, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyNextExchangeID)
	if bz == nil {
		return math.NewInt(1), nil
	}

	var id math.Int
	err := id.Unmarshal(bz)
	if err != nil {
		return math.NewInt(0), errorsmod.Wrapf(types.ErrInvalidID, "unable to unmarshal into exchange id %v", err)
	}

	return id, nil
}

func (k Keeper) IncrementNextExchangeID(ctx sdk.Context) error {
	store := ctx.KVStore(k.storeKey)

	currentID, err := k.GetNextExchangeID(ctx)
	if err != nil {
		return err
	}
	currentID = currentID.Add(math.NewInt(1))
	idBytes, err := currentID.Marshal()
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidID, "unable to marshal exchange id %v", err)
	}
	store.Set(types.KeyNextExchangeID, idBytes)

	return nil
}
