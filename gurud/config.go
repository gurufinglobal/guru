//go:build !test
// +build !test

package gurud

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/gurufinglobal/guru/v2/cmd/gurud/config"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// EVMOptionsFn defines a function type for setting app options specifically for
// the Cosmos EVM app. The function should receive the chainID and return an error if
// any.
type EVMOptionsFn func(uint64) error

var sealed = false

// EvmAppOptions sets up the base denomination for the chain.
// In cosmos/evm v0.5.1, the EVM coin info and EIP activators are initialized
// during InitGenesis via SetGlobalConfigVariables, so we do NOT call
// Configure() here to avoid "EVM coin info already set" errors.
func EvmAppOptions(chainID uint64) error {
	if sealed {
		return nil
	}

	coinInfo, found := config.ChainsCoinInfo[chainID]
	if !found {
		coinInfo, found = config.ChainsCoinInfo[config.CosmosChainID]
		if !found {
			return fmt.Errorf("unknown chain id: %d", chainID)
		}
	}

	// set the denom info for the chain
	if err := setBaseDenom(coinInfo); err != nil {
		return err
	}

	sealed = true
	return nil
}

// setBaseDenom registers the display denom and base denom and sets the
// base denom for the chain.
func setBaseDenom(ci evmtypes.EvmCoinInfo) error {
	if err := sdk.RegisterDenom(ci.DisplayDenom, math.LegacyOneDec()); err != nil {
		return err
	}

	// sdk.RegisterDenom will automatically overwrite the base denom when the
	// new setBaseDenom() are lower than the current base denom's units.
	return sdk.RegisterDenom(ci.Denom, math.LegacyNewDecWithPrec(1, int64(ci.Decimals)))
}
