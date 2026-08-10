package app

import (
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewNextBlockContext provides the SDK v0.54 test helper contract while this
// launch branch is pinned to SDK v0.53. It is test-only and writes directly to
// the mounted multistore, which is sufficient for fixtures that seed keeper
// state outside ABCI execution.
func (app *App) NewNextBlockContext(header cmtproto.Header) sdk.Context {
	return app.NewUncachedContext(false, header)
}

func repeatedByteAddress(value byte) []byte {
	address := make([]byte, 20)
	for i := range address {
		address[i] = value
	}
	return address
}
