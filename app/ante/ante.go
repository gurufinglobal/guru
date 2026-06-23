package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmante "github.com/cosmos/evm/ante"
)

// NewAnteHandler creates the chain ante handler from EVM handler options.
func NewAnteHandler(options evmante.HandlerOptions) (sdk.AnteHandler, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	return evmante.NewAnteHandler(options), nil
}
