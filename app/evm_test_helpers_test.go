package app

import (
	"sync"

	evmmodule "github.com/cosmos/evm/x/vm"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

var configureTestEVMOnce sync.Once

func configureTestEVM() {
	configureTestEVMOnce.Do(func() {
		evmmodule.SetGlobalConfigVariables(evmtypes.EvmCoinInfo{
			Denom:         appparams.BaseDenom,
			ExtendedDenom: appparams.BaseDenom,
			DisplayDenom:  appparams.DisplayDenom,
			Decimals:      18,
		})
	})
}

func repeatedByteAddress(value byte) []byte {
	address := make([]byte, 20)
	for i := range address {
		address[i] = value
	}
	return address
}
