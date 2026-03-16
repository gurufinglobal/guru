package ibc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	erc20types "github.com/cosmos/evm/x/erc20/types"
	"github.com/gurufinglobal/guru/v2/contracts"
	"github.com/gurufinglobal/guru/v2/gurud"
	evmibctesting "github.com/gurufinglobal/guru/v2/ibc/testing"
	"github.com/gurufinglobal/guru/v2/testutil"
)

// NativeErc20Info holds details about a deployed ERC20 token.
type NativeErc20Info struct {
	Denom        string
	ContractAbi  abi.ABI
	ContractAddr common.Address
	Account      common.Address // The address of the minter on the EVM chain
	InitialBal   *big.Int
}

// SetupNativeErc20 deploys, registers, and mints a native ERC20 token on an EVM-based chain.
func SetupNativeErc20(t *testing.T, chain *evmibctesting.TestChain) *NativeErc20Info {
	t.Helper()

	evmCtx := chain.GetContext()
	evmApp := chain.App.(*gurud.GURUD)

	tokenPairs := evmApp.Erc20Keeper.GetTokenPairs(evmCtx)
	if len(tokenPairs) == 0 {
		t.Fatal("no ERC20 token pairs available in genesis")
	}
	tokenPair := tokenPairs[0]
	contractAddr := tokenPair.GetERC20Contract()

	// Mint tokens to default sender
	contractAbi := contracts.ERC20MinterBurnerDecimalsContract.ABI
	nativeDenom := tokenPair.Denom
	sendAmt := ibctesting.DefaultCoinAmount
	senderAcc := chain.SenderAccount.GetAddress()
	stateDB := testutil.NewStateDB(evmCtx, evmApp.EVMKeeper)

	_, err := evmApp.EVMKeeper.CallEVM(
		evmCtx,
		stateDB,
		contractAbi,
		erc20types.ModuleAddress,
		contractAddr,
		true,
		false,
		nil,
		"mint",
		common.BytesToAddress(senderAcc),
		big.NewInt(sendAmt.Int64()),
	)
	if err != nil {
		t.Fatalf("mint call failed: %v", err)
	} else {
		t.Logf("mint call succeeded")
	}

	// Verify minted balance
	bal := evmApp.Erc20Keeper.BalanceOf(evmCtx, contractAbi, contractAddr, common.BytesToAddress(senderAcc))
	if bal.Cmp(big.NewInt(sendAmt.Int64())) != 0 {
		t.Fatalf("unexpected ERC20 balance; got %s, want %s", bal.String(), sendAmt.String())
	}

	return &NativeErc20Info{
		Denom:        nativeDenom,
		ContractAbi:  contractAbi,
		ContractAddr: contractAddr,
		Account:      common.BytesToAddress(senderAcc),
		InitialBal:   big.NewInt(sendAmt.Int64()),
	}
}
