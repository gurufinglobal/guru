package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"slices"
	"sync"
	"testing"
	"time"

	bankv1beta1 "cosmossdk.io/api/cosmos/bank/v1beta1"
	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	evidencetypes "cosmossdk.io/x/evidence/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	signing "github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	antetypes "github.com/cosmos/evm/ante/types"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	evmmempool "github.com/cosmos/evm/mempool"
	evmutils "github.com/cosmos/evm/utils"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethvm "github.com/ethereum/go-ethereum/core/vm"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	ethparams "github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	appante "github.com/gurufinglobal/guru/v2/app/ante"
	"github.com/gurufinglobal/guru/v2/config"
	constitutiontypes "github.com/gurufinglobal/guru/v2/x/constitution/types"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
	customstaking "github.com/gurufinglobal/guru/v2/x/staking"
)

// Cosmos EVM production configuration is process-global, so this test creates
// exactly one application instance per test process.
func TestApplicationStateMachine(t *testing.T) {
	const (
		testChainID    = "guru-test-1"
		testEVMChainID = uint64(9631)
	)
	var applicationLog bytes.Buffer
	application, err := New(Options{
		Logger:     log.NewLogger(&applicationLog, log.OutputJSONOption()),
		DB:         dbm.NewMemDB(),
		LoadLatest: true,
		HomePath:   t.TempDir(),
		ChainID:    testChainID,
		EVMChainID: testEVMChainID,
		BaseAppOptions: []func(*baseapp.BaseApp){
			baseapp.SetMempool(sdkmempool.NewSenderNonceMempool()),
		},
	})
	require.NoError(t, err)
	require.Equal(t, testChainID, application.ChainID())
	require.Equal(t, testEVMChainID, application.EVMChainID())
	require.IsType(t, sdkmempool.NoOpMempool{}, application.Mempool())
	require.NotNil(t, application.OracleProposalHandler)
	require.NotNil(t, application.oracleVoteHandler)
	require.NotNil(t, application.CustomStakingKeeper)
	require.Contains(t, application.ModuleManager.Modules, oracletypes.ModuleName)
	require.IsType(t, customstaking.AppModule{}, application.ModuleManager.Modules[stakingtypes.ModuleName])
	baseEncoding, err := MakeEncodingConfig()
	require.NoError(t, err)
	require.NotContains(
		t,
		baseEncoding.TxConfig.SignModeHandler().SupportedModes(),
		signingv1beta1.SignMode_SIGN_MODE_TEXTUAL,
	)
	textualTxConfig, err := NewTextualTxConfig(func(context.Context, string) (*bankv1beta1.Metadata, error) {
		return &bankv1beta1.Metadata{}, nil
	})
	require.NoError(t, err)
	require.Contains(
		t,
		textualTxConfig.SignModeHandler().SupportedModes(),
		signingv1beta1.SignMode_SIGN_MODE_TEXTUAL,
	)
	require.Contains(
		t,
		application.GetTxConfig().SignModeHandler().SupportedModes(),
		signingv1beta1.SignMode_SIGN_MODE_TEXTUAL,
	)
	serverMempool, ok := application.GetMempool().(*evmmempool.ExperimentalEVMMempool)
	require.True(t, ok)
	require.Nil(t, serverMempool)
	_, err = application.EvidenceKeeper.GetEvidenceHandler("unregistered")
	require.ErrorIs(t, err, evidencetypes.ErrNoEvidenceHandlerExists)

	require.True(t, sdk.DefaultPowerReduction.Equal(evmutils.AttoPowerReduction))
	require.Equal(t, config.BaseDenom, sdk.DefaultBondDenom)
	requireModulePrecedes(t, beginBlockerOrder, ibctransfertypes.ModuleName, erc20types.ModuleName)
	requireModulePrecedes(t, beginBlockerOrder, erc20types.ModuleName, feemarkettypes.ModuleName)
	requireModulePrecedes(t, beginBlockerOrder, feemarkettypes.ModuleName, evmtypes.ModuleName)
	requireModulePrecedes(t, beginBlockerOrder, evmtypes.ModuleName, constitutiontypes.ModuleName)
	requireModulePrecedes(t, beginBlockerOrder, constitutiontypes.ModuleName, distrtypes.ModuleName)
	requireModulePrecedes(t, endBlockerOrder, feemarkettypes.ModuleName, constitutiontypes.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, banktypes.ModuleName, constitutiontypes.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, constitutiontypes.ModuleName, oracletypes.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, oracletypes.ModuleName, evmtypes.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, constitutiontypes.ModuleName, evmtypes.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, authtypes.ModuleName, evmtypes.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, evmtypes.ModuleName, erc20types.ModuleName)
	requireModulePrecedes(t, initGenesisOrder, erc20types.ModuleName, ibctransfertypes.ModuleName)

	ethereumPrivateKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	cosmosPrivateKey, err := ethsecp256k1.GenerateKey()
	require.NoError(t, err)
	ethereumSender := sdk.AccAddress(ethereumPrivateKey.PubKey().Address())
	cosmosSender := sdk.AccAddress(cosmosPrivateKey.PubKey().Address())
	ethereumRecipient := common.HexToAddress("0x000000000000000000000000000000000000bEEF")
	cosmosRecipient := common.HexToAddress("0x000000000000000000000000000000000000CAfE")
	validatorSet, err := simtestutil.CreateRandomValidatorSet()
	require.NoError(t, err)
	proposerAddress := validatorSet.GetProposer().Address

	genesis := application.DefaultGenesis()
	require.Contains(t, genesis, oracletypes.ModuleName)
	distributionGenesis := distrtypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(genesis[distrtypes.ModuleName], distributionGenesis)
	require.True(t, distributionGenesis.Params.CommunityTax.IsZero())
	feeMarketGenesis := new(feemarkettypes.GenesisState)
	application.AppCodec().MustUnmarshalJSON(genesis[feemarkettypes.ModuleName], feeMarketGenesis)
	require.True(t, feeMarketGenesis.Params.NoBaseFee)
	require.True(t, feeMarketGenesis.Params.BaseFee.IsZero())
	require.True(t, feeMarketGenesis.Params.MinGasPrice.Equal(
		mustInt(constitutiontypes.MinGasPriceScaleFactor).ToLegacyDec(),
	))
	require.NoError(t, application.ConfigureConstitutionGenesis(
		genesis,
		ethereumSender.String(),
		sdk.AccAddress(ethereumRecipient.Bytes()).String(),
	))
	validatorAccount := authtypes.NewBaseAccount(
		sdk.AccAddress(validatorSet.Validators[0].Address),
		nil,
		0,
		0,
	)
	ethereumSenderAccount := authtypes.NewBaseAccount(ethereumSender, nil, 1, 0)
	ethereumRecipientAccount := authtypes.NewBaseAccount(
		sdk.AccAddress(ethereumRecipient.Bytes()), nil, 2, 0,
	)
	cosmosSenderAccount := authtypes.NewBaseAccount(cosmosSender, nil, 3, 0)
	cosmosRecipientAccount := authtypes.NewBaseAccount(
		sdk.AccAddress(cosmosRecipient.Bytes()), nil, 4, 0,
	)
	senderFunds := sdk.NewCoins(sdk.NewCoin(config.BaseDenom, mustInt("100000000000000000000")))
	genesisWithValidator, err := simtestutil.GenesisStateWithValSet(
		application.AppCodec(),
		genesis,
		validatorSet,
		[]authtypes.GenesisAccount{
			validatorAccount,
			ethereumSenderAccount,
			ethereumRecipientAccount,
			cosmosSenderAccount,
			cosmosRecipientAccount,
		},
		banktypes.Balance{Address: ethereumSender.String(), Coins: senderFunds},
		banktypes.Balance{Address: cosmosSender.String(), Coins: senderFunds},
	)
	require.NoError(t, err)
	genesis = GenesisState(genesisWithValidator)

	bankGenesis := banktypes.GetGenesisStateFromAppState(application.AppCodec(), genesis)
	bankGenesis.DenomMetadata = upsertNativeMetadata(bankGenesis.DenomMetadata)
	genesis[banktypes.ModuleName] = application.AppCodec().MustMarshalJSON(bankGenesis)
	require.NoError(t, application.ValidateGenesis(genesis))

	stakingGenesis := stakingtypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	require.Len(t, stakingGenesis.Validators, 1)
	require.Len(t, stakingGenesis.Delegations, 1)
	genesisSelfBond := stakingGenesis.Validators[0].TokensFromSharesTruncated(
		stakingGenesis.Delegations[0].GetShares(),
	).TruncateInt()
	require.True(t, genesisSelfBond.IsPositive())

	belowMinGenesis := cloneGenesisState(genesis)
	setGenesisMinValidatorBond(t, application, belowMinGenesis, genesisSelfBond.AddRaw(1))
	require.ErrorContains(
		t,
		application.ValidateGenesis(belowMinGenesis),
		"genesis self-bond "+genesisSelfBond.String()+" below constitution minimum "+genesisSelfBond.AddRaw(1).String(),
	)

	atMinGenesis := cloneGenesisState(genesis)
	setGenesisMinValidatorBond(t, application, atMinGenesis, genesisSelfBond)
	require.NoError(t, application.ValidateGenesis(atMinGenesis))

	duplicateSelfDelegationGenesis := cloneGenesisState(genesis)
	duplicateStakingGenesis := stakingtypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(
		duplicateSelfDelegationGenesis[stakingtypes.ModuleName],
		duplicateStakingGenesis,
	)
	duplicateStakingGenesis.Delegations = append(
		duplicateStakingGenesis.Delegations,
		duplicateStakingGenesis.Delegations[0],
	)
	duplicateSelfDelegationGenesis[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
		duplicateStakingGenesis,
	)
	require.ErrorContains(
		t,
		application.ValidateGenesis(duplicateSelfDelegationGenesis),
		"duplicate genesis self-delegation",
	)

	exportedInactiveGenesis := cloneGenesisState(belowMinGenesis)
	exportedInactiveStakingGenesis := stakingtypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(
		exportedInactiveGenesis[stakingtypes.ModuleName],
		exportedInactiveStakingGenesis,
	)
	exportedInactiveStakingGenesis.Exported = true
	exportedInactiveStakingGenesis.LastValidatorPowers = nil
	exportedInactiveGenesis[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
		exportedInactiveStakingGenesis,
	)
	require.NoError(t, application.ValidateGenesis(exportedInactiveGenesis))

	exportedActiveGenesis := cloneGenesisState(belowMinGenesis)
	exportedActiveStakingGenesis := stakingtypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(
		exportedActiveGenesis[stakingtypes.ModuleName],
		exportedActiveStakingGenesis,
	)
	exportedActiveStakingGenesis.Exported = true
	exportedActiveStakingGenesis.LastValidatorPowers = []stakingtypes.LastValidatorPower{
		{
			Address: exportedActiveStakingGenesis.Validators[0].GetOperator(),
			Power:   1,
		},
	}
	exportedActiveGenesis[stakingtypes.ModuleName] = application.AppCodec().MustMarshalJSON(
		exportedActiveStakingGenesis,
	)
	require.ErrorContains(t, application.ValidateGenesis(exportedActiveGenesis), "genesis self-bond")

	for _, tc := range []struct {
		name        string
		mutate      func(*feemarkettypes.Params)
		errorString string
	}{
		{
			name: "base fee enabled",
			mutate: func(params *feemarkettypes.Params) {
				params.NoBaseFee = false
			},
			errorString: "feemarket no_base_fee must be true",
		},
		{
			name: "non-zero base fee",
			mutate: func(params *feemarkettypes.Params) {
				params.BaseFee = feemarkettypes.DefaultBaseFee
			},
			errorString: "feemarket base_fee must be zero",
		},
		{
			name: "non-positive minimum gas price",
			mutate: func(params *feemarkettypes.Params) {
				params.MinGasPrice = sdkmath.LegacyZeroDec()
			},
			errorString: "feemarket min_gas_price must be positive",
		},
	} {
		t.Run("rejects feemarket policy with "+tc.name, func(t *testing.T) {
			invalidGenesis := cloneGenesisState(genesis)
			invalidFeeMarketGenesis := new(feemarkettypes.GenesisState)
			application.AppCodec().MustUnmarshalJSON(
				invalidGenesis[feemarkettypes.ModuleName],
				invalidFeeMarketGenesis,
			)
			tc.mutate(&invalidFeeMarketGenesis.Params)
			invalidGenesis[feemarkettypes.ModuleName] = application.AppCodec().MustMarshalJSON(
				invalidFeeMarketGenesis,
			)

			require.ErrorContains(t, application.ValidateGenesis(invalidGenesis), tc.errorString)
		})
	}

	vmGenesis := new(evmtypes.GenesisState)
	application.AppCodec().MustUnmarshalJSON(genesis[evmtypes.ModuleName], vmGenesis)
	require.Equal(t, config.BaseDenom, vmGenesis.Params.EvmDenom)
	require.Empty(t, vmGenesis.Params.ActiveStaticPrecompiles)
	require.Equal(t, []evmtypes.Preinstall{mustHistoryStoragePreinstall()}, vmGenesis.Preinstalls)

	for _, tc := range []struct {
		name        string
		mutate      func(*evmtypes.GenesisState)
		errorString string
	}{
		{
			name: "EVM denom",
			mutate: func(genesis *evmtypes.GenesisState) {
				genesis.Params.EvmDenom = "uother"
			},
			errorString: "evm evm_denom must be immutable config base denom",
		},
		{
			name: "extended denom",
			mutate: func(genesis *evmtypes.GenesisState) {
				genesis.Params.ExtendedDenomOptions.ExtendedDenom = "uother"
			},
			errorString: "evm extended_denom must be immutable config base denom",
		},
	} {
		t.Run("rejects mutable "+tc.name, func(t *testing.T) {
			invalidGenesis := cloneGenesisState(genesis)
			invalidEVMGenesis := new(evmtypes.GenesisState)
			application.AppCodec().MustUnmarshalJSON(
				invalidGenesis[evmtypes.ModuleName],
				invalidEVMGenesis,
			)
			tc.mutate(invalidEVMGenesis)
			invalidGenesis[evmtypes.ModuleName] = application.AppCodec().MustMarshalJSON(invalidEVMGenesis)

			require.ErrorContains(t, application.ValidateGenesis(invalidGenesis), tc.errorString)
		})
	}

	erc20Genesis := new(erc20types.GenesisState)
	application.AppCodec().MustUnmarshalJSON(genesis[erc20types.ModuleName], erc20Genesis)
	require.True(t, erc20Genesis.Params.EnableErc20)
	require.True(t, erc20Genesis.Params.PermissionlessRegistration)
	require.Empty(t, erc20Genesis.TokenPairs)
	require.Empty(t, erc20Genesis.Allowances)
	require.Empty(t, erc20Genesis.NativePrecompiles)
	require.Empty(t, erc20Genesis.DynamicPrecompiles)

	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	consensusParams := *simtestutil.DefaultConsensusParams
	consensusParams.Abci = &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1}
	_, err = application.InitChain(&abci.RequestInitChain{
		Time:            blockTime,
		ChainId:         testChainID,
		InitialHeight:   1,
		ConsensusParams: &consensusParams,
		AppStateBytes:   genesisBytes,
	})
	require.NoError(t, err)

	finalizeAndCommit(t, application, 1, blockTime.Add(time.Second), proposerAddress, nil)

	queryContext := committedContext(t, application)
	committedConsensusParams, err := application.ConsensusParamsKeeper.ParamsStore.Get(queryContext)
	require.NoError(t, err)
	require.NotNil(t, committedConsensusParams.Abci)
	require.Equal(t, int64(1), committedConsensusParams.Abci.VoteExtensionsEnableHeight)
	feeMarketParams := application.FeeMarketKeeper.GetParams(queryContext)
	require.True(t, feeMarketParams.NoBaseFee)
	require.True(t, feeMarketParams.BaseFee.IsZero())
	require.True(t, application.FeeMarketAdapter.GetMinGasPrice(
		queryContext,
	).Equal(mustInt(constitutiontypes.MinGasPriceScaleFactor).ToLegacyDec()))
	const ordinaryDynamicFloorGas = uint64(200_000)
	var ordinaryDynamicFloorTxBytes []byte
	t.Run("ordinary SDK dynamic fee enforces effective price floor", func(t *testing.T) {
		minGasPrice := feeMarketParams.MinGasPrice
		nominalFee := minGasPrice.
			MulInt(sdkmath.NewIntFromUint64(ordinaryDynamicFloorGas)).
			Ceil().
			RoundInt()
		feeCap := sdkmath.LegacyNewDecFromInt(nominalFee).
			QuoInt(sdkmath.NewIntFromUint64(ordinaryDynamicFloorGas))
		require.True(t, feeCap.GTE(minGasPrice))
		require.True(t, feeMarketParams.BaseFee.IsZero())

		transferCoin := sdk.NewInt64Coin(config.BaseDenom, 1)
		ordinaryMessage := &banktypes.MsgMultiSend{
			Inputs: []banktypes.Input{{
				Address: cosmosSender.String(),
				Coins:   sdk.NewCoins(transferCoin),
			}},
			Outputs: []banktypes.Output{{
				Address: sdk.AccAddress(cosmosRecipient.Bytes()).String(),
				Coins:   sdk.NewCoins(transferCoin),
			}},
		}
		dynamicExtension, extensionErr := codectypes.NewAnyWithValue(
			&antetypes.ExtensionOptionDynamicFeeTx{},
		)
		require.NoError(t, extensionErr)
		ordinaryDynamicFloorTxBytes = signAndEncodeCosmosTx(
			t,
			application,
			cosmosPrivateKey,
			cosmosSenderAccount.GetAccountNumber(),
			cosmosSenderAccount.GetSequence(),
			ordinaryMessage,
			sdk.NewCoins(sdk.NewCoin(config.BaseDenom, nominalFee)),
			ordinaryDynamicFloorGas,
			dynamicExtension,
		)

		type stateSnapshot struct {
			senderBalance    sdkmath.Int
			recipientBalance sdkmath.Int
			collectorBalance sdkmath.Int
			sequence         uint64
		}
		feeCollector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
		snapshot := func(ctx sdk.Context) stateSnapshot {
			account := application.AccountKeeper.GetAccount(ctx, cosmosSender)
			require.NotNil(t, account)
			return stateSnapshot{
				senderBalance: application.BankKeeper.GetBalance(
					ctx,
					cosmosSender,
					config.BaseDenom,
				).Amount,
				recipientBalance: application.BankKeeper.GetBalance(
					ctx,
					sdk.AccAddress(cosmosRecipient.Bytes()),
					config.BaseDenom,
				).Amount,
				collectorBalance: application.BankKeeper.GetBalance(
					ctx,
					feeCollector,
					config.BaseDenom,
				).Amount,
				sequence: account.GetSequence(),
			}
		}
		assertUnchanged := func(before stateSnapshot, ctx sdk.Context) {
			t.Helper()
			require.Equal(t, before, snapshot(ctx))
		}

		simulateBefore := snapshot(queryContext)
		_, _, simulateErr := application.Simulate(ordinaryDynamicFloorTxBytes)
		require.NoError(t, simulateErr)
		assertUnchanged(simulateBefore, committedContext(t, application))

		checkBefore := snapshot(application.GetContextForCheckTx(nil))
		checkResponse, checkErr := application.CheckTx(&abci.RequestCheckTx{
			Tx:   ordinaryDynamicFloorTxBytes,
			Type: abci.CheckTxType_New,
		})
		require.NoError(t, checkErr)
		require.Equal(t, sdkerrors.ErrInsufficientFee.ABCICode(), checkResponse.Code)
		assertUnchanged(checkBefore, application.GetContextForCheckTx(nil))

		recheckBefore := snapshot(application.GetContextForCheckTx(nil))
		recheckResponse, recheckErr := application.CheckTx(&abci.RequestCheckTx{
			Tx:   ordinaryDynamicFloorTxBytes,
			Type: abci.CheckTxType_Recheck,
		})
		require.NoError(t, recheckErr)
		require.Equal(t, sdkerrors.ErrInsufficientFee.ABCICode(), recheckResponse.Code)
		assertUnchanged(recheckBefore, application.GetContextForCheckTx(nil))

		proposalBefore := snapshot(committedContext(t, application))
		proposalResponse, proposalErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    [][]byte{ordinaryDynamicFloorTxBytes},
		})
		require.NoError(t, proposalErr)
		// The FixedSendGas proposal verifier preserves the SDK NoOp semantics for
		// decode-valid ordinary transactions, including ordinary ante failures.
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, proposalResponse.Status)
		assertUnchanged(proposalBefore, committedContext(t, application))
	})
	t.Run("oracle fee production wiring", func(t *testing.T) {
		assertOracleFeeMarketProductionWiring(t, application, queryContext)
	})
	const testGasPrice int64 = 700_000_000_000
	belowPegBytes, _ := signAndEncodeEthereumTx(
		t,
		application,
		ethereumPrivateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    0,
			To:       &ethereumRecipient,
			Gas:      100_000,
			GasPrice: big.NewInt(2_000_000_000),
		}),
	)
	belowPegResponse, err := application.CheckTx(&abci.RequestCheckTx{Tx: belowPegBytes})
	require.NoError(t, err)
	require.False(t, belowPegResponse.IsOK())
	validatorAddress, err := application.StakingKeeper.ValidatorAddressCodec().BytesToString(cosmosSender)
	require.NoError(t, err)
	belowMinSelfBondBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		cosmosSenderAccount.GetSequence(),
		&stakingtypes.MsgCreateValidator{
			DelegatorAddress: cosmosSender.String(),
			ValidatorAddress: validatorAddress,
			Value:            sdk.NewInt64Coin(config.BaseDenom, 9),
		},
		nil,
		0,
	)
	belowMinSelfBondResponse, err := application.CheckTx(&abci.RequestCheckTx{Tx: belowMinSelfBondBytes})
	require.NoError(t, err)
	require.False(t, belowMinSelfBondResponse.IsOK())
	require.Contains(t, belowMinSelfBondResponse.Log, "self-bond below minimum")
	require.True(t, application.EVMKeeper.IsContract(queryContext, ethparams.HistoryStorageAddress))
	historyCodeHash := application.EVMKeeper.GetCodeHash(queryContext, ethparams.HistoryStorageAddress)
	require.Equal(t, ethparams.HistoryStorageCode, application.EVMKeeper.GetCode(queryContext, historyCodeHash))
	for _, preinstall := range evmtypes.DefaultPreinstalls {
		address := common.HexToAddress(preinstall.Address)
		if address != ethparams.HistoryStorageAddress {
			require.False(t, application.EVMKeeper.IsContract(queryContext, address))
		}
	}
	erc20ModuleAccount := application.AccountKeeper.GetModuleAccount(queryContext, erc20types.ModuleName)
	require.NotNil(t, erc20ModuleAccount)
	require.ElementsMatch(t, []string{authtypes.Minter, authtypes.Burner}, erc20ModuleAccount.GetPermissions())
	require.True(t, application.BankKeeper.BlockedAddr(erc20ModuleAccount.GetAddress()))
	transferModuleAccount := application.AccountKeeper.GetModuleAccount(queryContext, ibctransfertypes.ModuleName)
	require.NotNil(t, transferModuleAccount)
	require.ElementsMatch(t, []string{authtypes.Minter, authtypes.Burner}, transferModuleAccount.GetPermissions())
	require.True(t, application.BankKeeper.BlockedAddr(transferModuleAccount.GetAddress()))
	constitutionModuleAccount := application.AccountKeeper.GetModuleAccount(queryContext, constitutiontypes.ModuleName)
	require.NotNil(t, constitutionModuleAccount)
	require.ElementsMatch(t, []string{authtypes.Burner}, constitutionModuleAccount.GetPermissions())
	require.True(t, application.BankKeeper.BlockedAddr(constitutionModuleAccount.GetAddress()))
	require.Equal(t, ibctransfertypes.DefaultParams(), application.TransferKeeper.GetParams(queryContext))
	require.Equal(t, erc20types.DefaultParams(), application.ERC20Keeper.GetParams(queryContext))
	require.Empty(t, application.ERC20Keeper.GetTokenPairs(queryContext))
	require.Empty(t, application.ERC20Keeper.GetNativePrecompiles(queryContext))
	require.Empty(t, application.ERC20Keeper.GetDynamicPrecompiles(queryContext))
	vmParams := application.EVMKeeper.GetParams(queryContext)
	require.Empty(t, vmParams.ActiveStaticPrecompiles)
	defaultExtensionAddresses := []common.Address{
		common.HexToAddress(evmtypes.P256PrecompileAddress),
		common.HexToAddress(evmtypes.Bech32PrecompileAddress),
		common.HexToAddress(evmtypes.StakingPrecompileAddress),
		common.HexToAddress(evmtypes.DistributionPrecompileAddress),
		common.HexToAddress(evmtypes.ICS20PrecompileAddress),
		common.HexToAddress(evmtypes.BankPrecompileAddress),
		common.HexToAddress(evmtypes.GovPrecompileAddress),
		common.HexToAddress(evmtypes.SlashingPrecompileAddress),
	}
	for address := range ethvm.PrecompiledContractsPrague {
		precompile, found, precompileErr := application.EVMKeeper.GetStaticPrecompileInstance(&vmParams, address)
		require.NoError(t, precompileErr)
		require.True(t, found, "Prague precompile %s is not installed", address)
		require.NotNil(t, precompile)
	}
	governanceParams := vmParams
	governanceParams.ActiveStaticPrecompiles = make([]string, len(defaultExtensionAddresses))
	for index, address := range defaultExtensionAddresses {
		governanceParams.ActiveStaticPrecompiles[index] = address.Hex()
	}
	slices.Sort(governanceParams.ActiveStaticPrecompiles)
	require.NoError(t, governanceParams.Validate())
	for _, address := range defaultExtensionAddresses {
		precompile, found, precompileErr := application.EVMKeeper.GetStaticPrecompileInstance(&governanceParams, address)
		require.NoError(t, precompileErr)
		require.True(t, found, "default precompile %s cannot be governance-activated", address)
		require.NotNil(t, precompile)
	}
	require.NotContains(t, vmParams.ActiveStaticPrecompiles, evmtypes.VestingPrecompileAddress)
	vesting, found, precompileErr := application.EVMKeeper.GetStaticPrecompileInstance(
		&vmParams,
		common.HexToAddress(evmtypes.VestingPrecompileAddress),
	)
	require.NoError(t, precompileErr)
	require.False(t, found)
	require.Nil(t, vesting)

	governanceContext, _ := queryContext.CacheContext()
	updateParams := &evmtypes.MsgUpdateParams{
		Authority: application.EVMKeeper.GetAuthority().String(),
		Params:    governanceParams,
	}
	updateHandler := application.MsgServiceRouter().Handler(updateParams)
	require.NotNil(t, updateHandler)
	_, err = updateHandler(governanceContext, updateParams)
	require.NoError(t, err)
	require.Equal(
		t,
		governanceParams.ActiveStaticPrecompiles,
		application.EVMKeeper.GetParams(governanceContext).ActiveStaticPrecompiles,
	)

	optionalPreinstalls := make([]evmtypes.Preinstall, 0, len(evmtypes.DefaultPreinstalls)-1)
	for _, preinstall := range evmtypes.DefaultPreinstalls {
		if common.HexToAddress(preinstall.Address) != ethparams.HistoryStorageAddress {
			optionalPreinstalls = append(optionalPreinstalls, preinstall)
		}
	}
	registerPreinstalls := &evmtypes.MsgRegisterPreinstalls{
		Authority:   authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Preinstalls: optionalPreinstalls,
	}
	registerHandler := application.MsgServiceRouter().Handler(registerPreinstalls)
	require.NotNil(t, registerHandler)
	_, err = registerHandler(governanceContext, registerPreinstalls)
	require.NoError(t, err)
	for _, preinstall := range optionalPreinstalls {
		address := common.HexToAddress(preinstall.Address)
		require.True(t, application.EVMKeeper.IsContract(governanceContext, address))
		codeHash := application.EVMKeeper.GetCodeHash(governanceContext, address)
		require.Equal(t, common.FromHex(preinstall.Code), application.EVMKeeper.GetCode(governanceContext, codeHash))
	}

	const submittedGas uint64 = 200_000
	gasPrice := big.NewInt(testGasPrice)
	transferValue := big.NewInt(12_345)
	minGasMultiplier := application.FeeMarketKeeper.GetParams(queryContext).MinGasMultiplier
	require.True(t, minGasMultiplier.Equal(feemarkettypes.DefaultMinGasMultiplier))
	ethereumGasUsed := minGasMultiplier.
		MulInt64(int64(submittedGas)).
		TruncateInt().Uint64()
	expectedBlockGasWanted := ethereumGasUsed + appante.StandardMsgSendGas
	ethereumProtocolFee := sdkmath.NewIntFromUint64(ethereumGasUsed).
		Mul(sdkmath.NewIntFromBigInt(gasPrice))
	fixedSendActualFee := sdkmath.NewIntFromUint64(appante.StandardMsgSendGas).
		Mul(sdkmath.NewIntFromBigInt(gasPrice))
	submittedFee := sdkmath.NewIntFromUint64(submittedGas).
		Mul(sdkmath.NewIntFromBigInt(gasPrice))

	ethereumBytes, ethereumTx := signAndEncodeEthereumTx(
		t,
		application,
		ethereumPrivateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    0,
			To:       &ethereumRecipient,
			Value:    transferValue,
			Gas:      submittedGas,
			GasPrice: gasPrice,
		}),
	)
	cosmosBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		cosmosSenderAccount.GetSequence(),
		banktypes.NewMsgSend(
			cosmosSender,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			sdk.NewCoins(sdk.NewCoin(
				config.BaseDenom,
				sdkmath.NewIntFromBigInt(transferValue),
			)),
		),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
		submittedGas,
	)
	cosmosGasOnlySimulationBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		cosmosSenderAccount.GetSequence(),
		banktypes.NewMsgSend(
			cosmosSender,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			sdk.NewCoins(sdk.NewCoin(
				config.BaseDenom,
				sdkmath.NewIntFromBigInt(transferValue),
			)),
		),
		nil,
		0,
	)
	t.Run("FixedSendGas D zero simulation is gas-only", func(t *testing.T) {
		beforeSender := application.BankKeeper.GetBalance(
			queryContext,
			cosmosSender,
			config.BaseDenom,
		)
		beforeRecipient := application.BankKeeper.GetBalance(
			queryContext,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			config.BaseDenom,
		)
		beforeSequence := application.AccountKeeper.GetAccount(queryContext, cosmosSender).GetSequence()

		gasInfo, _, simulateErr := application.Simulate(cosmosGasOnlySimulationBytes)
		require.NoError(t, simulateErr)
		require.Equal(t, appante.StandardMsgSendGas, gasInfo.GasWanted)
		require.Equal(t, appante.StandardMsgSendGas, gasInfo.GasUsed)

		afterContext := committedContext(t, application)
		require.Equal(t, beforeSender, application.BankKeeper.GetBalance(
			afterContext,
			cosmosSender,
			config.BaseDenom,
		))
		require.Equal(t, beforeRecipient, application.BankKeeper.GetBalance(
			afterContext,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			config.BaseDenom,
		))
		require.Equal(
			t,
			beforeSequence,
			application.AccountKeeper.GetAccount(afterContext, cosmosSender).GetSequence(),
		)
	})

	const proposalGas uint64 = 60_000_000
	proposalFee := sdkmath.NewIntFromUint64(proposalGas).
		Mul(sdkmath.NewIntFromBigInt(gasPrice))
	proposalEthereumBytes, _ := signAndEncodeEthereumTx(
		t,
		application,
		ethereumPrivateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    0,
			To:       &ethereumRecipient,
			Value:    transferValue,
			Gas:      proposalGas,
			GasPrice: gasPrice,
		}),
	)
	overflowEthereumBytes, _ := signAndEncodeEthereumTx(
		t,
		application,
		ethereumPrivateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    0,
			To:       &ethereumRecipient,
			Gas:      ^uint64(0),
			GasPrice: big.NewInt(1),
		}),
	)
	proposalCosmosBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		cosmosSenderAccount.GetSequence(),
		banktypes.NewMsgSend(
			cosmosSender,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			sdk.NewCoins(sdk.NewCoin(
				config.BaseDenom,
				sdkmath.NewIntFromBigInt(transferValue),
			)),
		),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, proposalFee)),
		proposalGas,
	)
	invalidFixedFee := feeMarketParams.MinGasPrice.
		MulInt(sdkmath.NewIntFromUint64(appante.StandardMsgSendGas)).
		Ceil().
		RoundInt().
		SubRaw(1)
	invalidFixedBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		cosmosSenderAccount.GetSequence(),
		banktypes.NewMsgSend(
			cosmosSender,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			sdk.NewCoins(sdk.NewCoin(
				config.BaseDenom,
				sdkmath.NewIntFromBigInt(transferValue),
			)),
		),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, invalidFixedFee)),
		appante.StandardMsgSendGas,
	)
	sequentialTransfer := senderFunds.AmountOf(config.BaseDenom).QuoRaw(2)
	sequentialFixedBytes := make([][]byte, 2)
	for sequence := range sequentialFixedBytes {
		sequentialFixedBytes[sequence] = signAndEncodeCosmosTx(
			t,
			application,
			cosmosPrivateKey,
			cosmosSenderAccount.GetAccountNumber(),
			uint64(sequence),
			banktypes.NewMsgSend(
				cosmosSender,
				sdk.AccAddress(cosmosRecipient.Bytes()),
				sdk.NewCoins(sdk.NewCoin(config.BaseDenom, sequentialTransfer)),
			),
			sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
			submittedGas,
		)
	}
	type proposalStateSnapshot struct {
		senderBalance       sdkmath.Int
		recipientBalance    sdkmath.Int
		feeCollectorBalance sdkmath.Int
		sequence            uint64
		blockGasWanted      uint64
		lastBlockHeight     int64
	}
	proposalSnapshot := func() proposalStateSnapshot {
		state := committedContext(t, application)
		account := application.AccountKeeper.GetAccount(state, cosmosSender)
		require.NotNil(t, account)
		return proposalStateSnapshot{
			senderBalance: application.BankKeeper.GetBalance(
				state,
				cosmosSender,
				config.BaseDenom,
			).Amount,
			recipientBalance: application.BankKeeper.GetBalance(
				state,
				sdk.AccAddress(cosmosRecipient.Bytes()),
				config.BaseDenom,
			).Amount,
			feeCollectorBalance: application.BankKeeper.GetBalance(
				state,
				authtypes.NewModuleAddress(authtypes.FeeCollectorName),
				config.BaseDenom,
			).Amount,
			sequence:        account.GetSequence(),
			blockGasWanted:  application.FeeMarketKeeper.GetBlockGasWanted(state),
			lastBlockHeight: application.LastBlockHeight(),
		}
	}

	t.Run("proposal rejects malformed raw transaction", func(t *testing.T) {
		malformed := []byte{0xff}
		prepared, prepareErr := application.PrepareProposal(&abci.RequestPrepareProposal{
			Height:     2,
			Time:       blockTime.Add(2 * time.Second),
			MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
			Txs:        [][]byte{malformed},
		})
		require.NoError(t, prepareErr)
		require.Empty(t, prepared.Txs)

		processed, processErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    [][]byte{proposalCosmosBytes, malformed},
		})
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, processed.Status)
	})

	t.Run("proposal rejects FixedSendGas fee admission failure", func(t *testing.T) {
		prepared, prepareErr := application.PrepareProposal(&abci.RequestPrepareProposal{
			Height:     2,
			Time:       blockTime.Add(2 * time.Second),
			MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
			Txs:        [][]byte{invalidFixedBytes},
		})
		require.NoError(t, prepareErr)
		require.Empty(t, prepared.Txs)

		processed, processErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    [][]byte{invalidFixedBytes},
		})
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, processed.Status)
	})

	t.Run("proposal snapshots ante sequence without applying message transfers", func(t *testing.T) {
		// Both transactions transfer half the committed balance. The second sequence
		// is valid only after the first ante succeeds, while applying the first
		// MsgSend would make the second A+F check fail.
		before := proposalSnapshot()
		prepared, prepareErr := application.PrepareProposal(&abci.RequestPrepareProposal{
			Height:     2,
			Time:       blockTime.Add(2 * time.Second),
			MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
			Txs:        sequentialFixedBytes,
		})
		require.NoError(t, prepareErr)
		require.Equal(t, sequentialFixedBytes, prepared.Txs)
		require.Equal(t, before, proposalSnapshot())

		processed, processErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    sequentialFixedBytes,
		})
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
		require.Equal(t, before, proposalSnapshot())
	})

	t.Run("proposal snapshots successful ante fee deductions", func(t *testing.T) {
		// The first message is a self-send, so its message execution has zero net
		// balance effect. The second requires the complete starting balance as A+F
		// and therefore fails only because the first successful ante deducted its fee.
		feeBoundaryTransfer := senderFunds.AmountOf(config.BaseDenom).Sub(submittedFee)
		require.True(t, feeBoundaryTransfer.IsPositive())
		feeBoundaryBytes := [][]byte{
			signAndEncodeCosmosTx(
				t,
				application,
				cosmosPrivateKey,
				cosmosSenderAccount.GetAccountNumber(),
				0,
				banktypes.NewMsgSend(
					cosmosSender,
					cosmosSender,
					sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, 1)),
				),
				sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
				submittedGas,
			),
			signAndEncodeCosmosTx(
				t,
				application,
				cosmosPrivateKey,
				cosmosSenderAccount.GetAccountNumber(),
				1,
				banktypes.NewMsgSend(
					cosmosSender,
					sdk.AccAddress(cosmosRecipient.Bytes()),
					sdk.NewCoins(sdk.NewCoin(config.BaseDenom, feeBoundaryTransfer)),
				),
				sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
				submittedGas,
			),
		}

		before := proposalSnapshot()
		prepared, prepareErr := application.PrepareProposal(&abci.RequestPrepareProposal{
			Height:     2,
			Time:       blockTime.Add(2 * time.Second),
			MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
			Txs:        feeBoundaryBytes,
		})
		require.NoError(t, prepareErr)
		require.Equal(t, feeBoundaryBytes[:1], prepared.Txs)
		require.Equal(t, before, proposalSnapshot())

		processed, processErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    feeBoundaryBytes,
		})
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, processed.Status)
		require.Equal(t, before, proposalSnapshot())
	})

	t.Run("proposal enforces the consensus block gas budget", func(t *testing.T) {
		overBudget := [][]byte{proposalEthereumBytes, proposalEthereumBytes}
		prepared, prepareErr := application.PrepareProposal(&abci.RequestPrepareProposal{
			Height:     2,
			Time:       blockTime.Add(2 * time.Second),
			MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
			Txs:        overBudget,
		})
		require.NoError(t, prepareErr)
		require.Equal(t, overBudget[:1], prepared.Txs)

		processed, processErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    overBudget,
		})
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, processed.Status)
	})

	t.Run("proposal rejects uint64 gas overflow when block gas is unlimited", func(t *testing.T) {
		unlimitedConsensusParams := *simtestutil.DefaultConsensusParams
		unlimitedBlockParams := *simtestutil.DefaultConsensusParams.Block
		unlimitedBlockParams.MaxGas = -1
		unlimitedConsensusParams.Block = &unlimitedBlockParams
		handler := newStandardMsgSendProposalHandler(application)
		baseProposalContext := committedContext(t, application).
			WithBlockHeight(2).
			WithBlockTime(blockTime.Add(2 * time.Second)).
			WithConsensusParams(unlimitedConsensusParams)
		overflowTxs := [][]byte{overflowEthereumBytes, overflowEthereumBytes}

		prepared, prepareErr := handler.PrepareProposal(
			baseProposalContext.WithExecMode(sdk.ExecModePrepareProposal),
			&abci.RequestPrepareProposal{
				Height:     2,
				Time:       blockTime.Add(2 * time.Second),
				MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
				Txs:        overflowTxs,
			},
		)
		require.NoError(t, prepareErr)
		require.Equal(t, overflowTxs[:1], prepared.Txs)

		processedPrepared, processErr := handler.ProcessProposal(
			baseProposalContext.WithExecMode(sdk.ExecModeProcessProposal),
			&abci.RequestProcessProposal{
				Height: 2,
				Time:   blockTime.Add(2 * time.Second),
				Txs:    prepared.Txs,
			},
		)
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processedPrepared.Status)

		processedOverflow, processErr := handler.ProcessProposal(
			baseProposalContext.WithExecMode(sdk.ExecModeProcessProposal),
			&abci.RequestProcessProposal{
				Height: 2,
				Time:   blockTime.Add(2 * time.Second),
				Txs:    overflowTxs,
			},
		)
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, processedOverflow.Status)
	})

	for _, proposalTxs := range [][][]byte{
		{proposalEthereumBytes, proposalCosmosBytes},
		{proposalCosmosBytes, proposalEthereumBytes},
	} {
		proposalGasResult, prepareErr := application.PrepareProposal(&abci.RequestPrepareProposal{
			Height:     2,
			Time:       blockTime.Add(2 * time.Second),
			MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
			Txs:        proposalTxs,
		})
		require.NoError(t, prepareErr)
		require.Equal(t, proposalTxs, proposalGasResult.Txs)

		processed, processErr := application.ProcessProposal(&abci.RequestProcessProposal{
			Height: 2,
			Time:   blockTime.Add(2 * time.Second),
			Txs:    proposalGasResult.Txs,
		})
		require.NoError(t, processErr)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
	}

	require.Zero(t, application.FeeMarketKeeper.GetTransientGasWanted(
		application.GetContextForCheckTx(nil),
	))
	for _, test := range []struct {
		name            string
		txBytes         []byte
		gasWanted       uint64
		simulateGasUsed uint64
		checkGasUsed    int64
	}{
		{
			name:            "ethereum",
			txBytes:         ethereumBytes,
			gasWanted:       submittedGas,
			simulateGasUsed: ethereumGasUsed,
		},
		{
			name:            "FixedSendGas MsgSend",
			txBytes:         cosmosBytes,
			gasWanted:       appante.StandardMsgSendGas,
			simulateGasUsed: appante.StandardMsgSendGas,
			checkGasUsed:    int64(appante.StandardMsgSendGas),
		},
	} {
		t.Run(test.name+" gas queries", func(t *testing.T) {
			gasInfo, _, simulateErr := application.Simulate(test.txBytes)
			require.NoError(t, simulateErr)
			require.Equal(t, test.gasWanted, gasInfo.GasWanted)
			require.Equal(t, test.simulateGasUsed, gasInfo.GasUsed)

			checkResult, checkErr := application.CheckTx(&abci.RequestCheckTx{
				Tx:   test.txBytes,
				Type: abci.CheckTxType_New,
			})
			require.NoError(t, checkErr)
			require.Equal(t, abci.CodeTypeOK, checkResult.Code, checkResult.Log)
			require.Equal(t, int64(test.gasWanted), checkResult.GasWanted)
			require.Equal(t, test.checkGasUsed, checkResult.GasUsed)
			require.Zero(t, application.FeeMarketKeeper.GetTransientGasWanted(
				application.GetContextForCheckTx(nil),
			))
		})
	}
	t.Run("ordinary SDK dynamic fee accepts exact floor through production ante", func(t *testing.T) {
		minGasPrice := feeMarketParams.MinGasPrice
		nominalFee := minGasPrice.
			MulInt(sdkmath.NewIntFromUint64(ordinaryDynamicFloorGas)).
			Ceil().
			RoundInt()
		feeCap := sdkmath.LegacyNewDecFromInt(nominalFee).
			QuoInt(sdkmath.NewIntFromUint64(ordinaryDynamicFloorGas))
		effectivePrice := sdkmath.LegacyMinDec(
			feeMarketParams.BaseFee.Add(minGasPrice),
			feeCap,
		)
		require.True(t, effectivePrice.Equal(minGasPrice))

		exactFloorExtension, extensionErr := codectypes.NewAnyWithValue(
			&antetypes.ExtensionOptionDynamicFeeTx{MaxPriorityPrice: minGasPrice},
		)
		require.NoError(t, extensionErr)
		checkContext := application.GetContextForCheckTx(nil)
		checkAccount := application.AccountKeeper.GetAccount(checkContext, cosmosSender)
		require.NotNil(t, checkAccount)
		exactFloorTxBytes := signAndEncodeCosmosTx(
			t,
			application,
			cosmosPrivateKey,
			checkAccount.GetAccountNumber(),
			checkAccount.GetSequence(),
			&banktypes.MsgMultiSend{
				Inputs: []banktypes.Input{{
					Address: cosmosSender.String(),
					Coins:   sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, 1)),
				}},
				Outputs: []banktypes.Output{{
					Address: sdk.AccAddress(cosmosRecipient.Bytes()).String(),
					Coins:   sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, 1)),
				}},
			},
			sdk.NewCoins(sdk.NewCoin(config.BaseDenom, nominalFee)),
			ordinaryDynamicFloorGas,
			exactFloorExtension,
		)

		exactFloorResult, checkErr := application.CheckTx(&abci.RequestCheckTx{
			Tx:   exactFloorTxBytes,
			Type: abci.CheckTxType_New,
		})
		require.NoError(t, checkErr)
		require.Equal(t, abci.CodeTypeOK, exactFloorResult.Code, exactFloorResult.Log)
		require.Equal(t, int64(ordinaryDynamicFloorGas), exactFloorResult.GasWanted)
	})

	prepareResult, err := application.PrepareProposal(&abci.RequestPrepareProposal{
		Height:     2,
		Time:       blockTime.Add(2 * time.Second),
		MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
		Txs:        [][]byte{ethereumBytes, cosmosBytes},
	})
	require.NoError(t, err)
	require.Equal(t, [][]byte{ethereumBytes, cosmosBytes}, prepareResult.Txs)
	processResult, err := application.ProcessProposal(&abci.RequestProcessProposal{
		Height: 2,
		Time:   blockTime.Add(2 * time.Second),
		Txs:    prepareResult.Txs,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processResult.Status)

	finalizeTxs := make([][]byte, 0, len(prepareResult.Txs)+1)
	finalizeTxs = append(finalizeTxs, ordinaryDynamicFloorTxBytes)
	finalizeTxs = append(finalizeTxs, prepareResult.Txs...)
	finalized, err := application.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:          2,
		Time:            blockTime.Add(2 * time.Second),
		ProposerAddress: proposerAddress,
		Txs:             finalizeTxs,
	})
	require.NoError(t, err)
	require.Len(t, finalized.TxResults, 3)
	rejectedDynamicResult := finalized.TxResults[0]
	require.Equal(t, sdkerrors.ErrInsufficientFee.ABCICode(), rejectedDynamicResult.Code)
	require.Equal(t, int64(ordinaryDynamicFloorGas), rejectedDynamicResult.GasWanted)
	require.Positive(t, rejectedDynamicResult.GasUsed)
	ethereumFinalizeResult := finalized.TxResults[1]
	require.True(t, ethereumFinalizeResult.IsOK(), ethereumFinalizeResult.Log)
	require.Equal(t, int64(submittedGas), ethereumFinalizeResult.GasWanted)
	require.Equal(t, int64(ethereumGasUsed), ethereumFinalizeResult.GasUsed)
	ethereumResult, decodeErr := evmtypes.DecodeTxResponse(ethereumFinalizeResult.Data)
	require.NoError(t, decodeErr)
	require.False(t, ethereumResult.Failed(), ethereumResult.VmError)
	require.Equal(t, ethereumTx.Hash().Hex(), ethereumResult.Hash)

	fixedSendFinalizeResult := finalized.TxResults[2]
	require.True(t, fixedSendFinalizeResult.IsOK(), fixedSendFinalizeResult.Log)
	require.Equal(t, int64(appante.StandardMsgSendGas), fixedSendFinalizeResult.GasWanted)
	require.Equal(t, int64(appante.StandardMsgSendGas), fixedSendFinalizeResult.GasUsed)
	requireFixedSendGasEvent(
		t,
		fixedSendFinalizeResult.Events,
		submittedGas,
		sdk.NewCoin(config.BaseDenom, submittedFee),
		gasPrice.String(),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, fixedSendActualFee)),
	)
	finalizeContext := application.GetContextForFinalizeBlock(nil)
	require.Zero(t, application.FeeMarketKeeper.GetTransientGasWanted(finalizeContext))
	expectedBlockGasWantedWithRejected := expectedBlockGasWanted + uint64(rejectedDynamicResult.GasUsed)
	require.Equal(
		t,
		expectedBlockGasWantedWithRejected,
		application.FeeMarketKeeper.GetBlockGasWanted(finalizeContext),
	)
	_, err = application.Commit()
	require.NoError(t, err)

	queryContext = committedContext(t, application)
	expectedEthereumSenderBalance := senderFunds.AmountOf(config.BaseDenom).
		Sub(sdkmath.NewIntFromBigInt(transferValue)).
		Sub(ethereumProtocolFee)
	expectedCosmosSenderBalance := senderFunds.AmountOf(config.BaseDenom).
		Sub(sdkmath.NewIntFromBigInt(transferValue)).
		Sub(fixedSendActualFee)
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		ethereumSender,
		config.BaseDenom,
	).Amount.Equal(expectedEthereumSenderBalance))
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		cosmosSender,
		config.BaseDenom,
	).Amount.Equal(expectedCosmosSenderBalance))
	for _, recipient := range []common.Address{ethereumRecipient, cosmosRecipient} {
		require.True(t, application.BankKeeper.GetBalance(
			queryContext,
			sdk.AccAddress(recipient.Bytes()),
			config.BaseDenom,
		).Amount.Equal(sdkmath.NewIntFromBigInt(transferValue)))
	}
	require.Equal(
		t,
		expectedBlockGasWantedWithRejected,
		application.FeeMarketKeeper.GetBlockGasWanted(queryContext),
	)

	senderEVMAddress := common.BytesToAddress(ethereumSender)
	require.Equal(t, uint64(1), application.EVMKeeper.GetNonce(queryContext, senderEVMAddress))
	require.Equal(t, uint64(1), application.AccountKeeper.GetAccount(queryContext, cosmosSender).GetSequence())

	const revertGasLimit uint64 = 100_000
	revertGasPrice := big.NewInt(testGasPrice)
	revertValue := big.NewInt(1_000_000_000_000_000_000)
	revertBytes, _ := signAndEncodeEthereumTx(
		t,
		application,
		ethereumPrivateKey,
		ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    1,
			Value:    revertValue,
			Gas:      revertGasLimit,
			GasPrice: revertGasPrice,
			Data:     common.FromHex("0x60006000fd"),
		}),
	)
	const blockedTransferAmount int64 = 9_876
	blockedFixedBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		1,
		banktypes.NewMsgSend(
			cosmosSender,
			erc20ModuleAccount.GetAddress(),
			sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, blockedTransferAmount)),
		),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
		submittedGas,
	)
	blockedProcessResult, err := application.ProcessProposal(&abci.RequestProcessProposal{
		Height: 3,
		Time:   blockTime.Add(3 * time.Second),
		Txs:    [][]byte{revertBytes, blockedFixedBytes},
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, blockedProcessResult.Status)

	senderBalanceBeforeRevert := application.BankKeeper.GetBalance(
		queryContext,
		ethereumSender,
		config.BaseDenom,
	)
	senderBalanceBeforeBlocked := application.BankKeeper.GetBalance(
		queryContext,
		cosmosSender,
		config.BaseDenom,
	)
	blockedBalanceBefore := application.BankKeeper.GetBalance(
		queryContext,
		erc20ModuleAccount.GetAddress(),
		config.BaseDenom,
	)
	finalized = finalizeAndCommit(
		t,
		application,
		3,
		blockTime.Add(3*time.Second),
		proposerAddress,
		[][]byte{revertBytes, blockedFixedBytes},
	)
	require.Len(t, finalized.TxResults, 2)
	require.True(t, finalized.TxResults[0].IsOK(), finalized.TxResults[0].Log)
	revertResult, err := evmtypes.DecodeTxResponse(finalized.TxResults[0].Data)
	require.NoError(t, err)
	require.True(t, revertResult.Failed())
	require.Equal(t, ethvm.ErrExecutionReverted.Error(), revertResult.VmError)
	blockedFixedResult := finalized.TxResults[1]
	require.False(t, blockedFixedResult.IsOK(), blockedFixedResult.Log)
	require.Contains(t, blockedFixedResult.Log, "not allowed to receive funds")
	require.Equal(t, int64(appante.StandardMsgSendGas), blockedFixedResult.GasWanted)
	require.Equal(t, int64(appante.StandardMsgSendGas), blockedFixedResult.GasUsed)
	requireFixedSendGasEvent(
		t,
		blockedFixedResult.Events,
		submittedGas,
		sdk.NewCoin(config.BaseDenom, submittedFee),
		gasPrice.String(),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, fixedSendActualFee)),
	)

	queryContext = committedContext(t, application)
	createdContract := ethcrypto.CreateAddress(senderEVMAddress, 1)
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(createdContract.Bytes()),
		config.BaseDenom,
	).IsZero())
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(ethereumRecipient.Bytes()),
		config.BaseDenom,
	).Amount.Equal(sdkmath.NewIntFromBigInt(transferValue)))
	require.Equal(t, uint64(2), application.EVMKeeper.GetNonce(queryContext, senderEVMAddress))
	senderBalanceAfterRevert := application.BankKeeper.GetBalance(
		queryContext,
		ethereumSender,
		config.BaseDenom,
	)
	revertCharge := senderBalanceBeforeRevert.Amount.Sub(senderBalanceAfterRevert.Amount)
	expectedRevertFee := sdkmath.NewIntFromUint64(revertResult.GasUsed).
		Mul(sdkmath.NewIntFromBigInt(revertGasPrice))
	require.True(t, revertCharge.Equal(expectedRevertFee))
	senderBalanceAfterBlocked := application.BankKeeper.GetBalance(
		queryContext,
		cosmosSender,
		config.BaseDenom,
	)
	require.True(t, senderBalanceBeforeBlocked.Amount.Sub(senderBalanceAfterBlocked.Amount).Equal(
		fixedSendActualFee,
	))
	require.Equal(t, blockedBalanceBefore, application.BankKeeper.GetBalance(
		queryContext,
		erc20ModuleAccount.GetAddress(),
		config.BaseDenom,
	))
	require.Equal(t, uint64(2), application.AccountKeeper.GetAccount(
		queryContext,
		cosmosSender,
	).GetSequence())
	require.Equal(
		t,
		uint64(revertResult.GasUsed)+appante.StandardMsgSendGas,
		application.FeeMarketKeeper.GetBlockGasWanted(queryContext),
	)
	require.Equal(t, int64(3), application.LastBlockHeight())

	messageEffectContext := committedContext(t, application)
	messageEffectSenderBefore := application.BankKeeper.GetBalance(
		messageEffectContext,
		cosmosSender,
		config.BaseDenom,
	)
	messageEffectRecipientBefore := application.BankKeeper.GetBalance(
		messageEffectContext,
		sdk.AccAddress(cosmosRecipient.Bytes()),
		config.BaseDenom,
	)
	messageEffectSequence := application.AccountKeeper.GetAccount(
		messageEffectContext,
		cosmosSender,
	).GetSequence()
	depletingTransfer := messageEffectSenderBefore.Amount.Sub(submittedFee)
	require.True(t, depletingTransfer.IsPositive())
	depletingFixedBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		messageEffectSequence,
		banktypes.NewMsgSend(
			cosmosSender,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			sdk.NewCoins(sdk.NewCoin(config.BaseDenom, depletingTransfer)),
		),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
		submittedGas,
	)
	dependentFixedBytes := signAndEncodeCosmosTx(
		t,
		application,
		cosmosPrivateKey,
		cosmosSenderAccount.GetAccountNumber(),
		messageEffectSequence+1,
		banktypes.NewMsgSend(
			cosmosSender,
			sdk.AccAddress(cosmosRecipient.Bytes()),
			sdk.NewCoins(sdk.NewInt64Coin(config.BaseDenom, 1)),
		),
		sdk.NewCoins(sdk.NewCoin(config.BaseDenom, submittedFee)),
		submittedGas,
	)
	// Proposal admission ignores the first MsgSend and admits both transactions.
	// Finalize executes it, leaving submittedFee-actualFee and causing the second
	// transaction's transfer-plus-submitted-fee check to fail per transaction.
	messageEffectProposal := [][]byte{depletingFixedBytes, dependentFixedBytes}
	messageEffectSnapshot := proposalSnapshot()
	preparedMessageEffect, err := application.PrepareProposal(&abci.RequestPrepareProposal{
		Height:     4,
		Time:       blockTime.Add(4 * time.Second),
		MaxTxBytes: simtestutil.DefaultConsensusParams.Block.MaxBytes,
		Txs:        messageEffectProposal,
	})
	require.NoError(t, err)
	require.Equal(t, messageEffectProposal, preparedMessageEffect.Txs)
	require.Equal(t, messageEffectSnapshot, proposalSnapshot())
	processedMessageEffect, err := application.ProcessProposal(&abci.RequestProcessProposal{
		Height: 4,
		Time:   blockTime.Add(4 * time.Second),
		Txs:    messageEffectProposal,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processedMessageEffect.Status)
	require.Equal(t, messageEffectSnapshot, proposalSnapshot())

	finalized = finalizeAndCommit(
		t,
		application,
		4,
		blockTime.Add(4*time.Second),
		proposerAddress,
		messageEffectProposal,
	)
	require.Len(t, finalized.TxResults, 2)
	require.True(t, finalized.TxResults[0].IsOK(), finalized.TxResults[0].Log)
	require.Equal(t, int64(appante.StandardMsgSendGas), finalized.TxResults[0].GasWanted)
	require.Equal(t, int64(appante.StandardMsgSendGas), finalized.TxResults[0].GasUsed)
	dependentFixedResult := finalized.TxResults[1]
	require.False(t, dependentFixedResult.IsOK(), dependentFixedResult.Log)
	require.Equal(
		t,
		sdkerrors.ErrInsufficientFunds.ABCICode(),
		dependentFixedResult.Code,
	)
	require.Contains(t, dependentFixedResult.Log, "FixedSendGas spendable balance")
	require.Equal(t, int64(appante.StandardMsgSendGas), dependentFixedResult.GasWanted)
	require.Equal(t, int64(appante.StandardMsgSendGas), dependentFixedResult.GasUsed)
	requireNoFixedSendGasEvent(t, dependentFixedResult.Events)

	queryContext = committedContext(t, application)
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		cosmosSender,
		config.BaseDenom,
	).Amount.Equal(
		messageEffectSenderBefore.Amount.
			Sub(fixedSendActualFee).
			Sub(depletingTransfer),
	))
	require.True(t, application.BankKeeper.GetBalance(
		queryContext,
		sdk.AccAddress(cosmosRecipient.Bytes()),
		config.BaseDenom,
	).Amount.Equal(messageEffectRecipientBefore.Amount.Add(depletingTransfer)))
	require.Equal(t, messageEffectSequence+1, application.AccountKeeper.GetAccount(
		queryContext,
		cosmosSender,
	).GetSequence())
	require.Equal(
		t,
		appante.StandardMsgSendGas,
		application.FeeMarketKeeper.GetBlockGasWanted(queryContext),
	)
	require.Equal(t, int64(4), application.LastBlockHeight())

	exported, err := application.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(5), exported.Height)
	var exportedGenesis GenesisState
	require.NoError(t, json.Unmarshal(exported.AppState, &exportedGenesis))
	require.NoError(t, application.ValidateGenesis(exportedGenesis))

	t.Run("missed minimum gas price schedule is discarded by the application end blocker", func(t *testing.T) {
		committedHeight := application.LastBlockHeight()
		require.Equal(t, int64(3), committedHeight)

		beforeCtx := committedContext(t, application)
		beforeParams := application.FeeMarketKeeper.GetParams(beforeCtx)

		// Seed the only state that current future-height validation cannot produce
		// through a normal block: a valid schedule that existed before its effective
		// height but was not processed there. This models a legacy/recovered store
		// condition while leaving the production ModuleManager and keeper graph intact.
		seedCtx := application.NewUncachedContext(false, cmtproto.Header{
			ChainID: testChainID,
			Height:  committedHeight,
			Time:    blockTime.Add(time.Duration(committedHeight) * time.Second),
		}).WithEventManager(sdk.NewEventManager())
		require.NoError(t, application.ConstitutionKeeper.AfterOracleValueApplied(
			seedCtx,
			&oracletypes.OracleValue{
				Symbol:        config.MinGasPriceOracleSymbol,
				ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
				Value:         "0.5",
				BlockHeight:   committedHeight,
				BlockTimeUnix: blockTime.Add(time.Duration(committedHeight) * time.Second).Unix(),
			},
			1,
		))
		seededSchedule, err := application.ConstitutionKeeper.GetMinGasPriceSchedule(seedCtx)
		require.NoError(t, err)
		require.Equal(t, int64(4), seededSchedule.GetEffectiveHeight())
		require.False(t, beforeParams.MinGasPrice.Equal(
			sdkmath.LegacyMustNewDecFromStr(seededSchedule.GetScheduledMinGasPrice()),
		))

		var storeTrace synchronizedBuffer
		application.SetCommitMultiStoreTracer(&storeTrace)
		defer application.SetCommitMultiStoreTracer(nil)
		applicationLog.Reset()

		missedResult := finalizeAndCommit(
			t,
			application,
			4,
			blockTime.Add(4*time.Second),
			proposerAddress,
			nil,
		)
		missedEvents := matchingABCIEvents(
			missedResult.Events,
			constitutiontypes.EventTypeMinGasPriceUpdateSkipped,
			constitutiontypes.AttributeKeyReason,
			constitutiontypes.MinGasPriceUpdateReasonMissedEffectiveHeight,
		)
		require.Len(t, missedEvents, 1)
		require.True(t, abciEventHasAttribute(
			missedEvents[0], constitutiontypes.AttributeKeyHeight, "4",
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0], constitutiontypes.AttributeKeyObservedHeight, "4",
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0], constitutiontypes.AttributeKeyNextHeight, "5",
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0], constitutiontypes.AttributeKeyEffectiveHeight, "4",
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0],
			constitutiontypes.AttributeKeyCurrentMinGasPrice,
			beforeParams.MinGasPrice.String(),
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0],
			constitutiontypes.AttributeKeyScheduledMinGasPrice,
			seededSchedule.GetScheduledMinGasPrice(),
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0],
			constitutiontypes.AttributeKeyPendingMinGasPrice,
			seededSchedule.GetScheduledMinGasPrice(),
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0], constitutiontypes.AttributeKeySourceOracleHeight, "3",
		))
		require.True(t, abciEventHasAttribute(
			missedEvents[0], constitutiontypes.AttributeKeyPendingDelayBlocks, "1",
		))

		warningLog := applicationLog.String()
		require.Contains(t, warningLog, `"level":"warn"`)
		require.Contains(t, warningLog, "discarding missed minimum gas price schedule")
		require.Contains(t, warningLog, `"reason":"missed_effective_height"`)
		require.Contains(t, warningLog, `"observed_height":4`)
		require.Contains(t, warningLog, `"effective_height":4`)
		require.Contains(t, warningLog, `"next_height":5`)
		require.Contains(t, warningLog, `"scheduled_min_gas_price":"`+seededSchedule.GetScheduledMinGasPrice()+`"`)

		afterMissedCtx := committedContext(t, application)
		require.Equal(t, beforeParams, application.FeeMarketKeeper.GetParams(afterMissedCtx))
		_, err = application.ConstitutionKeeper.GetMinGasPriceSchedule(afterMissedCtx)
		require.ErrorIs(t, err, collections.ErrNotFound)
		require.Zero(t, countStoreWrites(
			t,
			storeTrace.Bytes(),
			feemarkettypes.StoreKey,
			feemarkettypes.ParamsKey,
		), "missed handling must not call the FeeMarket parameter setter")

		storeTrace.Reset()
		applicationLog.Reset()
		repeatedResult := finalizeAndCommit(
			t,
			application,
			5,
			blockTime.Add(5*time.Second),
			proposerAddress,
			nil,
		)
		require.Empty(t, matchingABCIEvents(
			repeatedResult.Events,
			constitutiontypes.EventTypeMinGasPriceUpdateSkipped,
			constitutiontypes.AttributeKeyReason,
			constitutiontypes.MinGasPriceUpdateReasonMissedEffectiveHeight,
		))
		require.NotContains(t, applicationLog.String(), constitutiontypes.MinGasPriceUpdateReasonMissedEffectiveHeight)
		afterRepeatCtx := committedContext(t, application)
		require.Equal(t, beforeParams, application.FeeMarketKeeper.GetParams(afterRepeatCtx))
		_, err = application.ConstitutionKeeper.GetMinGasPriceSchedule(afterRepeatCtx)
		require.ErrorIs(t, err, collections.ErrNotFound)
		require.Zero(t, countStoreWrites(
			t,
			storeTrace.Bytes(),
			feemarkettypes.StoreKey,
			feemarkettypes.ParamsKey,
		), "the cleared schedule must not trigger a later FeeMarket parameter write")
		require.Equal(t, int64(5), application.LastBlockHeight())
	})

}

func requireFixedSendGasEvent(
	t *testing.T,
	events []abci.Event,
	declaredGas uint64,
	submittedFee sdk.Coin,
	effectiveGasPrice string,
	actualFee sdk.Coins,
) {
	t.Helper()

	var fixedEvent *abci.Event
	for index := range events {
		if events[index].Type == appante.EventTypeFixedSendGas {
			fixedEvent = &events[index]
			break
		}
	}
	require.NotNil(t, fixedEvent)

	attributes := make(map[string]string, len(fixedEvent.Attributes))
	for _, attribute := range fixedEvent.Attributes {
		attributes[attribute.Key] = attribute.Value
	}
	require.Equal(t, map[string]string{
		appante.AttributeKeyDeclaredGas:       new(big.Int).SetUint64(declaredGas).String(),
		appante.AttributeKeyAccountingGas:     new(big.Int).SetUint64(appante.StandardMsgSendGas).String(),
		appante.AttributeKeySubmittedFee:      submittedFee.String(),
		appante.AttributeKeyEffectiveGasPrice: effectiveGasPrice,
		appante.AttributeKeyActualFee:         actualFee.String(),
	}, attributes)
}

func requireNoFixedSendGasEvent(t *testing.T, events []abci.Event) {
	t.Helper()
	for _, event := range events {
		require.NotEqual(t, appante.EventTypeFixedSendGas, event.Type)
	}
}

func assertOracleFeeMarketProductionWiring(
	t *testing.T,
	application *App,
	baseCtx sdk.Context,
) {
	t.Helper()
	ctx, _ := baseCtx.CacheContext()
	ctx = ctx.WithBlockHeight(20).WithEventManager(sdk.NewEventManager())
	initialParams := application.FeeMarketKeeper.GetParams(ctx)

	require.NoError(t, application.OracleKeeper.SetTaskDefinition(ctx, &oracletypes.OracleTask{
		Symbol:             config.MinGasPriceOracleSymbol,
		ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 2,
	}))
	require.NoError(t, application.OracleKeeper.ApplyOracleValues(ctx, []*oracletypes.OracleValue{{
		Symbol:        config.MinGasPriceOracleSymbol,
		ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Value:         "0.5",
		BlockHeight:   20,
		BlockTimeUnix: 1_700_000_000,
	}}))

	schedule, err := application.ConstitutionKeeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(22), schedule.GetEffectiveHeight())
	require.Equal(t, uint32(2), schedule.GetSourceSubmissionIntervalBlocks())
	require.Equal(t, "693000000000.000000000000000000", schedule.GetScheduledMinGasPrice())
	require.Equal(t, initialParams, application.FeeMarketKeeper.GetParams(ctx))

	require.NoError(t, application.ConstitutionKeeper.ApplyDueMinGasPriceSchedule(ctx))
	require.Equal(t, initialParams, application.FeeMarketKeeper.GetParams(ctx))
	pending, err := application.ConstitutionKeeper.GetMinGasPriceSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, schedule, pending)

	dueCtx := ctx.WithBlockHeight(21).WithEventManager(sdk.NewEventManager())
	require.NoError(t, application.ConstitutionKeeper.ApplyDueMinGasPriceSchedule(dueCtx))
	expectedParams := initialParams
	expectedParams.MinGasPrice = sdkmath.LegacyMustNewDecFromStr("693000000000")
	require.Equal(t, expectedParams, application.FeeMarketKeeper.GetParams(dueCtx))
	_, err = application.ConstitutionKeeper.GetMinGasPriceSchedule(dueCtx)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func cloneGenesisState(genesis GenesisState) GenesisState {
	cloned := make(GenesisState, len(genesis))
	for moduleName, state := range genesis {
		cloned[moduleName] = slices.Clone(state)
	}
	return cloned
}

func setGenesisMinValidatorBond(
	t *testing.T,
	application *App,
	genesis GenesisState,
	amount sdkmath.Int,
) {
	t.Helper()
	constitutionGenesis := new(constitutiontypes.GenesisState)
	application.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.Params.MinValidatorBondAmount = &sdk.Coin{
		Denom:  config.BaseDenom,
		Amount: amount,
	}
	genesis[constitutiontypes.ModuleName] = application.AppCodec().MustMarshalJSON(constitutionGenesis)
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

func matchingABCIEvents(
	events []abci.Event,
	eventType string,
	attributeKey string,
	attributeValue string,
) []abci.Event {
	matched := make([]abci.Event, 0, 1)
	for _, event := range events {
		if event.Type == eventType && abciEventHasAttribute(event, attributeKey, attributeValue) {
			matched = append(matched, event)
		}
	}
	return matched
}

func abciEventHasAttribute(event abci.Event, key, value string) bool {
	for _, attribute := range event.Attributes {
		if attribute.Key == key && attribute.Value == value {
			return true
		}
	}
	return false
}

// synchronizedBuffer keeps the multistore tracer valid if writes are emitted
// concurrently. Production file-backed trace writers are concurrency-safe,
// whereas bytes.Buffer is not.
type synchronizedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *synchronizedBuffer) Bytes() []byte {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return bytes.Clone(buffer.buffer.Bytes())
}

func (buffer *synchronizedBuffer) Reset() {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	buffer.buffer.Reset()
}

func countStoreWrites(t *testing.T, trace []byte, storeName string, key []byte) int {
	t.Helper()
	encodedKey := base64.StdEncoding.EncodeToString(key)
	writes := 0
	for _, line := range bytes.Split(bytes.TrimSpace(trace), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var operation struct {
			Operation string         `json:"operation"`
			Key       string         `json:"key"`
			Metadata  map[string]any `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal(line, &operation))
		if operation.Operation == "write" &&
			operation.Key == encodedKey &&
			operation.Metadata["store_name"] == storeName {
			writes++
		}
	}
	return writes
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
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(application.EVMChainID()))
	signed, err := ethtypes.SignTx(unsigned, signer, ecdsaKey)
	require.NoError(t, err)

	message := new(evmtypes.MsgEthereumTx)
	require.NoError(t, message.FromSignedEthereumTx(signed, signer))
	tx, err := message.BuildTx(application.GetTxConfig().NewTxBuilder(), config.BaseDenom)
	require.NoError(t, err)
	encoded, err := application.GetTxConfig().TxEncoder()(tx)
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
	extensionOptions ...*codectypes.Any,
) []byte {
	t.Helper()
	builder := application.GetTxConfig().NewTxBuilder()
	if len(extensionOptions) > 0 {
		extensionBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
		require.True(t, ok)
		extensionBuilder.SetExtensionOptions(extensionOptions...)
	}
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
		ChainID:       application.ChainID(),
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
		application.GetTxConfig(),
		sequence,
	)
	require.NoError(t, err)
	require.NoError(t, builder.SetSignatures(signature))
	encoded, err := application.GetTxConfig().TxEncoder()(builder.GetTx())
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
