//go:build !test

package app

import (
	"context"
	"encoding/json"
	"math/big"
	"slices"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signing "github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	evmutils "github.com/cosmos/evm/utils"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethvm "github.com/ethereum/go-ethereum/core/vm"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/gurufinglobal/guru/v2/config"
)

// Cosmos EVM production configuration is process-global, so this test creates
// exactly one application instance per test process.
func TestApplicationStateMachine(t *testing.T) {
	application, err := New(Options{
		Logger:   log.NewNopLogger(),
		DB:       dbm.NewMemDB(),
		HomePath: t.TempDir(),
	})
	require.NoError(t, err)

	require.True(t, sdk.DefaultPowerReduction.Equal(evmutils.AttoPowerReduction))
	require.Equal(t, config.BaseDenom, sdk.DefaultBondDenom)
	requireModulePrecedes(t, ModuleOrderBeginBlockers(), feemarkettypes.ModuleName, evmtypes.ModuleName)
	requireModulePrecedes(t, ModuleOrderInitGenesis(), authtypes.ModuleName, evmtypes.ModuleName)

	privateKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	sender := sdk.AccAddress(privateKey.PubKey().Address())
	recipient := common.HexToAddress("0x000000000000000000000000000000000000bEEF")
	validatorSet, err := simtestutil.CreateRandomValidatorSet()
	require.NoError(t, err)
	proposerAddress := validatorSet.GetProposer().Address

	genesis := application.DefaultGenesis()
	validatorAccount := authtypes.NewBaseAccount(
		sdk.AccAddress(validatorSet.Validators[0].Address),
		nil,
		0,
		0,
	)
	senderAccount := authtypes.NewBaseAccount(sender, nil, 1, 0)
	recipientAccount := authtypes.NewBaseAccount(sdk.AccAddress(recipient.Bytes()), nil, 2, 0)
	senderFunds := sdk.NewCoins(sdk.NewCoin(config.BaseDenom, mustInt("10000000000000000000")))
	genesisWithValidator, err := simtestutil.GenesisStateWithValSet(
		application.AppCodec(),
		genesis,
		validatorSet,
		[]authtypes.GenesisAccount{validatorAccount, senderAccount, recipientAccount},
		banktypes.Balance{Address: sender.String(), Coins: senderFunds},
	)
	require.NoError(t, err)
	genesis = GenesisState(genesisWithValidator)

	bankGenesis := banktypes.GetGenesisStateFromAppState(application.AppCodec(), genesis)
	bankGenesis.DenomMetadata = upsertNativeMetadata(bankGenesis.DenomMetadata)
	genesis[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankGenesis)
	require.NoError(t, application.ValidateGenesis(genesis))

	vmGenesis := new(evmtypes.GenesisState)
	application.AppCodec().MustUnmarshalJSON(genesis[evmtypes.ModuleName], vmGenesis)
	require.Equal(t, config.BaseDenom, vmGenesis.Params.EvmDenom)
	require.Empty(t, vmGenesis.Params.ActiveStaticPrecompiles)

	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	_, err = application.InitChain(&abci.RequestInitChain{
		Time:            blockTime,
		ChainId:         config.LocalChainID,
		InitialHeight:   1,
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   genesisBytes,
	})
	require.NoError(t, err)

	finalizeAndCommit(t, application, 1, blockTime.Add(time.Second), proposerAddress, nil)

	queryContext := committedContext(t, application)
	vmParams := application.EVMKeeper.GetParams(queryContext)
	require.Empty(t, vmParams.ActiveStaticPrecompiles)
	require.Len(t, application.installedPrecompiles, len(ethvm.PrecompiledContractsPrague))
	for address := range ethvm.PrecompiledContractsPrague {
		precompile, found, precompileErr := application.EVMKeeper.GetStaticPrecompileInstance(&vmParams, address)
		require.NoError(t, precompileErr)
		require.True(t, found, "Prague precompile %s is not installed", address)
		require.NotNil(t, precompile)
	}

	amount := big.NewInt(12_345)
	successBytes, successTx := signAndEncodeEthereumTx(
		t,
		application,
		privateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    0,
			To:       &recipient,
			Value:    amount,
			Gas:      100_000,
			GasPrice: big.NewInt(2_000_000_000),
		}),
	)
	finalized := finalizeAndCommit(
		t,
		application,
		2,
		blockTime.Add(2*time.Second),
		proposerAddress,
		[][]byte{successBytes},
	)
	require.Len(t, finalized.TxResults, 1)
	require.True(t, finalized.TxResults[0].IsOK(), finalized.TxResults[0].Log)
	successResult, err := evmtypes.DecodeTxResponse(finalized.TxResults[0].Data)
	require.NoError(t, err)
	require.False(t, successResult.Failed(), successResult.VmError)
	require.Equal(t, successTx.Hash().Hex(), successResult.Hash)

	queryContext = committedContext(t, application)
	recipientBalance := application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(recipient.Bytes()),
		config.BaseDenom,
	)
	require.Equal(t, amount.String(), recipientBalance.Amount.String())
	senderBalanceAfterSuccess := application.BankKeeper.GetBalance(queryContext, sender, config.BaseDenom)
	amountWithoutFee := senderFunds.AmountOf(config.BaseDenom).Sub(sdkmath.NewIntFromBigInt(amount))
	require.True(t, senderBalanceAfterSuccess.Amount.LT(amountWithoutFee))
	senderEVMAddress := common.BytesToAddress(sender)
	require.Equal(t, uint64(1), application.EVMKeeper.GetNonce(queryContext, senderEVMAddress))

	const revertGasLimit uint64 = 100_000
	revertGasPrice := big.NewInt(2_000_000_000)
	revertValue := big.NewInt(1_000_000_000_000_000_000)
	revertBytes, _ := signAndEncodeEthereumTx(
		t,
		application,
		privateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    1,
			Value:    revertValue,
			Gas:      revertGasLimit,
			GasPrice: revertGasPrice,
			Data:     common.FromHex("0x60006000fd"),
		}),
	)
	finalized = finalizeAndCommit(
		t,
		application,
		3,
		blockTime.Add(3*time.Second),
		proposerAddress,
		[][]byte{revertBytes},
	)
	require.Len(t, finalized.TxResults, 1)
	require.True(t, finalized.TxResults[0].IsOK(), finalized.TxResults[0].Log)
	revertResult, err := evmtypes.DecodeTxResponse(finalized.TxResults[0].Data)
	require.NoError(t, err)
	require.True(t, revertResult.Failed())
	require.Equal(t, ethvm.ErrExecutionReverted.Error(), revertResult.VmError)

	queryContext = committedContext(t, application)
	createdContract := ethcrypto.CreateAddress(senderEVMAddress, 1)
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(createdContract.Bytes()),
		config.BaseDenom,
	).IsZero())
	require.Equal(t, amount.String(), application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(recipient.Bytes()),
		config.BaseDenom,
	).Amount.String())
	require.Equal(t, uint64(2), application.EVMKeeper.GetNonce(queryContext, senderEVMAddress))
	senderBalanceAfterRevert := application.BankKeeper.GetBalance(queryContext, sender, config.BaseDenom)
	revertCharge := senderBalanceAfterSuccess.Amount.Sub(senderBalanceAfterRevert.Amount)
	expectedRevertFee := sdkmath.NewIntFromUint64(revertResult.GasUsed).Mul(sdkmath.NewIntFromBigInt(revertGasPrice))
	require.True(t, revertCharge.Equal(expectedRevertFee))

	senderAccountAfterRevert := application.AccountKeeper.GetAccount(queryContext, sender)
	require.NotNil(t, senderAccountAfterRevert)
	require.Equal(t, uint64(2), senderAccountAfterRevert.GetSequence())
	cosmosValue := sdkmath.NewInt(777)
	cosmosFee := sdk.NewCoins(sdk.NewCoin(config.BaseDenom, mustInt("1000000000000000")))
	cosmosBytes := signAndEncodeCosmosTx(
		t,
		application,
		privateKey,
		senderAccountAfterRevert.GetAccountNumber(),
		senderAccountAfterRevert.GetSequence(),
		banktypes.NewMsgSend(
			sender,
			sdk.AccAddress(recipient.Bytes()),
			sdk.NewCoins(sdk.NewCoin(config.BaseDenom, cosmosValue)),
		),
		cosmosFee,
		200_000,
	)
	finalized = finalizeAndCommit(
		t,
		application,
		4,
		blockTime.Add(4*time.Second),
		proposerAddress,
		[][]byte{cosmosBytes},
	)
	require.Len(t, finalized.TxResults, 1)
	require.True(t, finalized.TxResults[0].IsOK(), finalized.TxResults[0].Log)

	queryContext = committedContext(t, application)
	finalRecipientAmount := sdkmath.NewIntFromBigInt(amount).Add(cosmosValue)
	require.Equal(t, finalRecipientAmount.String(), application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(recipient.Bytes()),
		config.BaseDenom,
	).Amount.String())
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		sender,
		config.BaseDenom,
	).Amount.Equal(senderBalanceAfterRevert.Amount.Sub(cosmosValue).Sub(cosmosFee.AmountOf(config.BaseDenom))))
	require.Equal(t, uint64(3), application.AccountKeeper.GetAccount(queryContext, sender).GetSequence())
	require.Equal(t, uint64(3), application.EVMKeeper.GetNonce(queryContext, senderEVMAddress))
	require.Equal(t, int64(4), application.LastBlockHeight())
}

func finalizeAndCommit(
	t *testing.T,
	application *App,
	height int64,
	blockTime time.Time,
	proposerAddress []byte,
	txs [][]byte,
) *abci.ResponseFinalizeBlock {
	t.Helper()
	response, err := application.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:          height,
		Time:            blockTime,
		ProposerAddress: proposerAddress,
		Txs:             txs,
	})
	require.NoError(t, err)
	_, err = application.Commit()
	require.NoError(t, err)
	return response
}

func committedContext(t *testing.T, application *App) sdk.Context {
	t.Helper()
	ctx, err := application.CreateQueryContext(0, false)
	require.NoError(t, err)
	return ctx
}

func signAndEncodeEthereumTx(
	t *testing.T,
	application *App,
	privateKey *ethsecp256k1.PrivKey,
	unsigned *ethtypes.Transaction,
) ([]byte, *ethtypes.Transaction) {
	t.Helper()
	ecdsaKey, err := privateKey.ToECDSA()
	require.NoError(t, err)
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(config.EVMChainID))
	signed, err := ethtypes.SignTx(unsigned, signer, ecdsaKey)
	require.NoError(t, err)

	message := new(evmtypes.MsgEthereumTx)
	require.NoError(t, message.FromSignedEthereumTx(signed, signer))
	tx, err := message.BuildTx(application.TxConfig().NewTxBuilder(), config.BaseDenom)
	require.NoError(t, err)
	encoded, err := application.TxConfig().TxEncoder()(tx)
	require.NoError(t, err)
	return encoded, signed
}

func signAndEncodeCosmosTx(
	t *testing.T,
	application *App,
	privateKey cryptotypes.PrivKey,
	accountNumber uint64,
	sequence uint64,
	message sdk.Msg,
	fees sdk.Coins,
	gasLimit uint64,
) []byte {
	t.Helper()
	builder := application.TxConfig().NewTxBuilder()
	require.NoError(t, builder.SetMsgs(message))
	builder.SetFeeAmount(fees)
	builder.SetGasLimit(gasLimit)

	signMode := signing.SignMode_SIGN_MODE_DIRECT
	require.NoError(t, builder.SetSignatures(signing.SignatureV2{
		PubKey: privateKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode: signMode,
		},
		Sequence: sequence,
	}))
	signerData := xauthsigning.SignerData{
		Address:       sdk.AccAddress(privateKey.PubKey().Address()).String(),
		ChainID:       config.LocalChainID,
		AccountNumber: accountNumber,
		Sequence:      sequence,
		PubKey:        privateKey.PubKey(),
	}
	signature, err := clienttx.SignWithPrivKey(
		context.Background(),
		signMode,
		signerData,
		builder,
		privateKey,
		application.TxConfig(),
		sequence,
	)
	require.NoError(t, err)
	require.NoError(t, builder.SetSignatures(signature))
	encoded, err := application.TxConfig().TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return encoded
}

func requireModulePrecedes(t *testing.T, order []string, before string, after string) {
	t.Helper()
	beforeIndex := slices.Index(order, before)
	afterIndex := slices.Index(order, after)
	require.NotEqual(t, -1, beforeIndex, "module %q is missing from order", before)
	require.NotEqual(t, -1, afterIndex, "module %q is missing from order", after)
	require.Less(t, beforeIndex, afterIndex)
}
