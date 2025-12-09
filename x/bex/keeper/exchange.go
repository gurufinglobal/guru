package keeper

import (
	"github.com/gurufinglobal/guru/v2/x/bex/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"cosmossdk.io/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

func (k Keeper) GetExchange(ctx sdk.Context, id math.Int) (*types.Exchange, error) {
	store := ctx.KVStore(k.storeKey)
	exchangeStore := prefix.NewStore(store, types.KeyExchanges)

	idBytes, err := id.Marshal()
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidId, "unable to marshal exchange id %v", err)
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

func (k Keeper) GetExchangesByOracleRequestId(ctx sdk.Context, oracleRequestId uint64) ([]types.Exchange, error) {
	allExchanges, _, err := k.GetPaginatedExchanges(ctx, &query.PageRequest{Limit: query.PaginationMaxLimit})
	if err != nil {
		return nil, err
	}

	exchanges := []types.Exchange{}
	for _, exchange := range allExchanges {
		if exchange.OracleRequestId == oracleRequestId {
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
		return errorsmod.Wrapf(types.ErrInvalidId, "unable to marshal exchange id %v", err)
	}

	// check if the exchange already exists
	bz := exchangeStore.Get(idBytes)
	if bz == nil {

		// check the next exchange id
		nextId, err := k.GetNextExchangeId(ctx)
		if err != nil {
			return err
		}
		if !nextId.Equal(exchange.Id) {
			return errorsmod.Wrapf(types.ErrInvalidId, "expected: %v, got: %v", nextId, exchange.Id)
		}

		// increment the next exchange id
		err = k.IncrementNextExchangeId(ctx)
		if err != nil {
			return err
		}
	}

	exchangeBytes := k.cdc.MustMarshal(exchange)
	exchangeStore.Set(idBytes, exchangeBytes)

	return nil
}

func (k Keeper) GetNextExchangeId(ctx sdk.Context) (math.Int, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.KeyNextExchangeId)
	if bz == nil {
		return math.NewInt(1), nil
	}

	var id math.Int
	err := id.Unmarshal(bz)
	if err != nil {
		return math.NewInt(0), errorsmod.Wrapf(types.ErrInvalidId, "unable to unmarshal into exchange id %v", err)
	}

	return id, nil
}

func (k Keeper) IncrementNextExchangeId(ctx sdk.Context) error {
	store := ctx.KVStore(k.storeKey)

	currentId, err := k.GetNextExchangeId(ctx)
	if err != nil {
		return err
	}
	currentId = currentId.Add(math.NewInt(1))
	idBytes, err := currentId.Marshal()
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidId, "unable to marshal exchange id %v", err)
	}
	store.Set(types.KeyNextExchangeId, idBytes)

	return nil
}
