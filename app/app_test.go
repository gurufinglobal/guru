package app

import (
	"context"
	"encoding/json"
	"math/big"
	"slices"
	"testing"
	"time"

	bankv1beta1 "cosmossdk.io/api/cosmos/bank/v1beta1"
	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	evidencetypes "cosmossdk.io/x/evidence/types"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	signing "github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
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
	application, err := New(Options{
		Logger:     log.NewNopLogger(),
		DB:         dbm.NewMemDB(),
		LoadLatest: true,
		HomePath:   t.TempDir(),
		ChainID:    testChainID,
		EVMChainID: testEVMChainID,
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

	governanceDenomGenesis := cloneGenesisState(genesis)
	governanceDenomVM := new(evmtypes.GenesisState)
	application.AppCodec().MustUnmarshalJSON(governanceDenomGenesis[evmtypes.ModuleName], governanceDenomVM)
	governanceDenomVM.Params.EvmDenom = "uother"
	governanceDenomVM.Params.ExtendedDenomOptions.ExtendedDenom = "uother"
	governanceDenomGenesis[evmtypes.ModuleName] = application.AppCodec().MustMarshalJSON(governanceDenomVM)
	require.NoError(t, application.ValidateGenesis(governanceDenomGenesis))

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
	_, err = application.InitChain(&abci.RequestInitChain{
		Time:            blockTime,
		ChainId:         testChainID,
		InitialHeight:   1,
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   genesisBytes,
	})
	require.NoError(t, err)

	finalizeAndCommit(t, application, 1, blockTime.Add(time.Second), proposerAddress, nil)

	queryContext := committedContext(t, application)
	feeMarketParams := application.FeeMarketKeeper.GetParams(queryContext)
	require.True(t, feeMarketParams.NoBaseFee)
	require.True(t, feeMarketParams.BaseFee.IsZero())
	require.True(t, application.FeeMarketAdapter.GetMinGasPrice(
		sdk.WrapSDKContext(queryContext),
	).Equal(mustInt(constitutiontypes.MinGasPriceScaleFactor).ToLegacyDec()))
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
	minimumGasUsed := minGasMultiplier.
		MulInt64(int64(submittedGas)).
		TruncateInt().Uint64()
	expectedBlockGasWanted := minGasMultiplier.
		MulInt64(int64(2 * submittedGas)).
		TruncateInt().Uint64()
	protocolFee := sdkmath.NewIntFromUint64(minimumGasUsed).Mul(sdkmath.NewIntFromBigInt(gasPrice))
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
		require.Equal(t, proposalTxs[:1], proposalGasResult.Txs)
	}

	require.Zero(t, application.FeeMarketKeeper.GetTransientGasWanted(
		application.GetContextForCheckTx(nil),
	))
	for _, test := range []struct {
		name    string
		txBytes []byte
	}{
		{name: "ethereum", txBytes: ethereumBytes},
		{name: "msgsend", txBytes: cosmosBytes},
	} {
		t.Run(test.name+" gas queries", func(t *testing.T) {
			gasInfo, _, simulateErr := application.Simulate(test.txBytes)
			require.NoError(t, simulateErr)
			require.Equal(t, submittedGas, gasInfo.GasWanted)
			require.Equal(t, minimumGasUsed, gasInfo.GasUsed)

			checkResult, checkErr := application.CheckTx(&abci.RequestCheckTx{
				Tx:   test.txBytes,
				Type: abci.CheckTxType_New,
			})
			require.NoError(t, checkErr)
			require.Equal(t, abci.CodeTypeOK, checkResult.Code, checkResult.Log)
			require.Equal(t, int64(submittedGas), checkResult.GasWanted)
			require.Zero(t, checkResult.GasUsed)
			require.Zero(t, application.FeeMarketKeeper.GetTransientGasWanted(
				application.GetContextForCheckTx(nil),
			))
		})
	}

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

	finalized, err := application.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:          2,
		Time:            blockTime.Add(2 * time.Second),
		ProposerAddress: proposerAddress,
		Txs:             prepareResult.Txs,
	})
	require.NoError(t, err)
	require.Len(t, finalized.TxResults, 2)
	for index, result := range finalized.TxResults {
		require.True(t, result.IsOK(), result.Log)
		require.Equal(t, int64(submittedGas), result.GasWanted)
		require.Equal(t, int64(minimumGasUsed), result.GasUsed)
		if index == 0 {
			ethereumResult, decodeErr := evmtypes.DecodeTxResponse(result.Data)
			require.NoError(t, decodeErr)
			require.False(t, ethereumResult.Failed(), ethereumResult.VmError)
			require.Equal(t, ethereumTx.Hash().Hex(), ethereumResult.Hash)
		}
	}
	finalizeContext := application.GetContextForFinalizeBlock(nil)
	require.Zero(t, application.FeeMarketKeeper.GetTransientGasWanted(finalizeContext))
	require.Equal(
		t,
		expectedBlockGasWanted,
		application.FeeMarketKeeper.GetBlockGasWanted(finalizeContext),
	)
	_, err = application.Commit()
	require.NoError(t, err)

	queryContext = committedContext(t, application)
	expectedSenderBalance := senderFunds.AmountOf(config.BaseDenom).
		Sub(sdkmath.NewIntFromBigInt(transferValue)).
		Sub(protocolFee)
	for _, sender := range []sdk.AccAddress{ethereumSender, cosmosSender} {
		require.True(t, application.BankKeeper.GetBalance(
			queryContext,
			sender,
			config.BaseDenom,
		).Amount.Equal(expectedSenderBalance))
	}
	for _, recipient := range []common.Address{ethereumRecipient, cosmosRecipient} {
		require.True(t, application.BankKeeper.GetBalance(
			queryContext,
			sdk.AccAddress(recipient.Bytes()),
			config.BaseDenom,
		).Amount.Equal(sdkmath.NewIntFromBigInt(transferValue)))
	}
	require.Equal(
		t,
		expectedBlockGasWanted,
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
	senderBalanceBeforeRevert := application.BankKeeper.GetBalance(
		queryContext,
		ethereumSender,
		config.BaseDenom,
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
	require.Equal(t, int64(3), application.LastBlockHeight())

	exported, err := application.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(4), exported.Height)
	var exportedGenesis GenesisState
	require.NoError(t, json.Unmarshal(exported.AppState, &exportedGenesis))
	require.NoError(t, application.ValidateGenesis(exportedGenesis))
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
) []byte {
	t.Helper()
	builder := application.GetTxConfig().NewTxBuilder()
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
