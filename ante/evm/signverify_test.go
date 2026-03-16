package evm_test

import (
	"math/big"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	ethante "github.com/gurufinglobal/guru/v2/ante/evm"
	"github.com/gurufinglobal/guru/v2/testutil"
	testutiltx "github.com/gurufinglobal/guru/v2/testutil/tx"
)

func (suite *AnteTestSuite) TestEthSigVerificationDecorator() {
	addr, privKey := testutiltx.NewAddrKey()
	ethCfg := evmtypes.GetEthChainConfig()
	ethSigner := ethtypes.LatestSignerForChainID(ethCfg.ChainID)

	ethContractCreationTxParams := &evmtypes.EvmTxArgs{
		ChainID:  ethCfg.ChainID,
		Nonce:    1,
		Amount:   big.NewInt(10),
		GasLimit: 1000,
		GasPrice: big.NewInt(1),
	}
	signedTx := evmtypes.NewTx(ethContractCreationTxParams)
	signedTx.From = addr.Bytes()
	err := signedTx.Sign(ethSigner, testutiltx.NewSigner(privKey))
	suite.Require().NoError(err)

	unprotectedEthTxParams := &evmtypes.EvmTxArgs{
		Nonce:    1,
		Amount:   big.NewInt(10),
		GasLimit: 1000,
		GasPrice: big.NewInt(1),
	}
	unprotectedTx := evmtypes.NewTx(unprotectedEthTxParams)
	unprotectedTx.From = addr.Bytes()
	err = unprotectedTx.Sign(ethtypes.HomesteadSigner{}, testutiltx.NewSigner(privKey))
	suite.Require().NoError(err)

	testCases := []struct {
		name      string
		tx        sdk.Tx
		reCheckTx bool
		expPass   bool
	}{
		{"ReCheckTx", &testutiltx.InvalidTx{}, true, false},
		{"invalid transaction type", &testutiltx.InvalidTx{}, false, false},
		{
			"invalid sender",
			evmtypes.NewTx(&evmtypes.EvmTxArgs{
				To:       &addr,
				Nonce:    1,
				Amount:   big.NewInt(10),
				GasLimit: 1000,
				GasPrice: big.NewInt(1),
			}),
			false,
			false,
		},
		{"successful signature verification", signedTx, false, true},
		{"invalid unprotected tx", unprotectedTx, false, true},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			dec := ethante.NewEthSigVerificationDecorator(suite.GetNetwork().App.EVMKeeper)
			_, err := dec.AnteHandle(suite.GetNetwork().GetContext().WithIsReCheckTx(tc.reCheckTx), tc.tx, false, testutil.NoOpNextFn)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
