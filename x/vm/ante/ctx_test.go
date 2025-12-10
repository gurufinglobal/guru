package ante_test

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	evmante "github.com/gurufinglobal/guru/v2/x/vm/ante"
)

func (suite *EvmAnteTestSuite) TestBuildEvmExecutionCtx() {
	network := network.New()

	ctx := evmante.BuildEvmExecutionCtx(network.GetContext())

	suite.Equal(storetypes.GasConfig{}, ctx.KVGasConfig())
	suite.Equal(storetypes.GasConfig{}, ctx.TransientKVGasConfig())
}
