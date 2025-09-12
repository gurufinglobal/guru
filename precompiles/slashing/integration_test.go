package slashing_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"

	//nolint:revive,ST1001 // dot imports are fine for Ginkgo
	. "github.com/onsi/ginkgo/v2"
	//nolint:revive,ST1001 // dot imports are fine for Ginkgo
	. "github.com/onsi/gomega"

	cmn "github.com/GPTx-global/guru-v2/v2/precompiles/common"
	"github.com/GPTx-global/guru-v2/v2/precompiles/slashing/testdata"
	"github.com/GPTx-global/guru-v2/v2/precompiles/testutil"
	"github.com/GPTx-global/guru-v2/v2/testutil/integration/os/factory"
	testutils "github.com/GPTx-global/guru-v2/v2/testutil/integration/os/utils"
	evmtypes "github.com/GPTx-global/guru-v2/v2/x/vm/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestPrecompileIntegrationTestSuite(t *testing.T) {
	// Run Ginkgo integration tests
	RegisterFailHandler(Fail)
	RunSpecs(t, "Staking Precompile Integration Tests")
}

// General variables used for integration tests
var (
	// valAddr is validator address used for testing
	valAddr sdk.ValAddress

	// gasPrice is the gas price used for the transactions
	gasPrice = math.NewInt(1e9)
	// callArgs  are the default arguments for calling the smart contract
	//
	// NOTE: this has to be populated in a BeforeEach block because the contractAddr would otherwise be a nil address.
	callArgs factory.CallArgs

	// defaultLogCheck instantiates a log check arguments struct with the precompile ABI events populated.
	defaultLogCheck testutil.LogCheckArgs
	// txArgs are the EVM transaction arguments to use in the transactions
	txArgs evmtypes.EvmTxArgs
)

var _ = Describe("Calling slashing precompile from contract", Ordered, func() {
	var s *PrecompileTestSuite

	var (
		slashingCallerContract evmtypes.CompiledContract
		// contractAddr is the address of the smart contract that will be deployed
		contractAddr common.Address
		err          error

		// execRevertedCheck defines the default log checking arguments which includes the
		// standard revert message.
		execRevertedCheck testutil.LogCheckArgs
	)

	BeforeAll(func() {
		slashingCallerContract, err = testdata.LoadSlashingCallerContract()
		Expect(err).To(BeNil(), "error while loading the smart contract: %v", err)
	})

	BeforeEach(func() {
		s = new(PrecompileTestSuite)
		s.SetupTest()

		valAddr, err = sdk.ValAddressFromBech32(s.network.GetValidators()[0].GetOperator())
		Expect(err).To(BeNil())

		// send funds to the contract
		err := testutils.FundAccountWithBaseDenom(s.factory, s.network, s.keyring.GetKey(0), contractAddr.Bytes(), math.NewInt(2e18))
		Expect(err).To(BeNil())
		Expect(s.network.NextBlock()).To(BeNil())

		contractAddr, err = s.factory.DeployContract(
			s.keyring.GetPrivKey(0),
			evmtypes.EvmTxArgs{}, // NOTE: passing empty struct to use default values
			factory.ContractDeploymentData{
				Contract: slashingCallerContract,
			},
		)
		Expect(err).To(BeNil(), "error while deploying the smart contract: %v", err)
		Expect(s.network.NextBlock()).To(BeNil(), "error calling NextBlock: %v", err)

		// check contract was correctly deployed
		cAcc := s.network.App.EVMKeeper.GetAccount(s.network.GetContext(), contractAddr)
		Expect(cAcc).ToNot(BeNil(), "contract account should exist")
		Expect(cAcc.IsContract()).To(BeTrue(), "account should be a contract")

		// populate default call args
		callArgs = factory.CallArgs{
			ContractABI: slashingCallerContract.ABI,
		}

		// reset tx args each test to avoid keeping custom
		// values of previous tests (e.g. gasLimit)
		txArgs = evmtypes.EvmTxArgs{
			To:       &contractAddr,
			GasPrice: gasPrice.BigInt(),
		}

		// default log check arguments
		defaultLogCheck = testutil.LogCheckArgs{ABIEvents: s.precompile.Events}
		execRevertedCheck = defaultLogCheck.WithErrContains("execution reverted")
	})

	// =====================================
	// 				TRANSACTIONS
	// =====================================
	Context("unjail", func() {
		BeforeEach(func() {
			// withdraw address should be same as address
			res, err := s.grpcHandler.GetDelegatorWithdrawAddr(s.keyring.GetAccAddr(0).String())
			Expect(err).To(BeNil(), "error while calling the precompile")
			Expect(res.WithdrawAddress).To(Equal(s.keyring.GetAccAddr(0).String()))

			// populate default arguments
			callArgs.MethodName = "testUnjail"
		})

		It("should fail if sender is not jailed validator", func() {
			txArgs = evmtypes.EvmTxArgs{
				To: &contractAddr,
			}
			callArgs.Args = []interface{}{
				common.BytesToAddress(valAddr.Bytes()),
			}

			revertReasonCheck := execRevertedCheck.WithErrNested(
				cmn.ErrRequesterIsNotMsgSender,
				contractAddr,
				common.BytesToAddress(valAddr.Bytes()),
			)

			_, _, err := s.factory.CallContractAndCheckLogs(
				s.keyring.GetPrivKey(0),
				txArgs,
				callArgs,
				revertReasonCheck,
			)
			Expect(err).To(BeNil(), "error while calling the smart contract: %v", err)
		})
	})
})
