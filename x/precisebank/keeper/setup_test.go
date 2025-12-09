package keeper_test

import (
	"testing"

	testconstants "github.com/gurufinglobal/guru/v2/testutil/constants"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/factory"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/grpc"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/keyring"
	"github.com/gurufinglobal/guru/v2/testutil/integration/os/network"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
	"github.com/stretchr/testify/suite"
)

const SEED = int64(42)

type KeeperIntegrationTestSuite struct {
	suite.Suite

	network *network.UnitTestNetwork
	factory factory.TxFactory
	keyring keyring.Keyring
}

func TestKeeperIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperIntegrationTestSuite))
}

func (suite *KeeperIntegrationTestSuite) SetupTest() {
	suite.SetupTestWithChainID(testconstants.SixDecimalsChainID)
}

func (suite *KeeperIntegrationTestSuite) SetupTestWithChainID(chainID testconstants.ChainID) {
	suite.keyring = keyring.New(2)

	// Reset evm config here for the standard case
	configurator := evmtypes.NewEVMConfigurator()
	configurator.ResetTestConfig()
	err := configurator.
		WithEVMCoinInfo(testconstants.ExampleChainCoinInfo[chainID]).
		Configure()
	if err != nil {
		return
	}

	nw := network.NewUnitTestNetwork(
		network.WithChainID(chainID),
		network.WithPreFundedAccounts(suite.keyring.GetAllAccAddrs()...),
	)
	gh := grpc.NewIntegrationHandler(nw)
	tf := factory.New(nw, gh)

	suite.network = nw
	suite.factory = tf
}
