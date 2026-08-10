package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdksecp256k1 "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

const oracleProposalFinalizeTestChainID = "guru-oracle-proposal-finalize-1"

func TestOracleProposalEnvelopeDecodesButAnteRejectsUserSubmission(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		false,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	payloadTx, err := oracleabci.EncodeProposalTx(&oracletypes.OracleProposalPayload{Height: 7})
	require.NoError(t, err)

	tx, err := testApp.TxConfig().TxDecoder()(payloadTx)
	require.NoError(t, err)
	require.Empty(t, tx.GetMsgs())

	_, err = testApp.anteHandler(sdk.Context{}, tx, false)
	require.ErrorIs(t, err, sdkerrors.ErrUnknownExtensionOptions)
	require.ErrorContains(t, err, "reserved for consensus records")
}

func TestAnteRejectsOracleProposalOptionInNonCriticalList(t *testing.T) {
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		false,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(appparams.SDKChainID),
	)
	option, err := codectypes.NewAnyWithValue(&oracletypes.OracleProposalPayload{Height: 7})
	require.NoError(t, err)
	builder := testApp.TxConfig().NewTxBuilder()
	extensionBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
	require.True(t, ok)
	extensionBuilder.SetNonCriticalExtensionOptions(option)
	txBytes, err := testApp.TxConfig().TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	tx, err := testApp.TxConfig().TxDecoder()(txBytes)
	require.NoError(t, err)

	_, isCandidate, err := oracleabci.DecodeProposalTx(txBytes)
	require.True(t, isCandidate)
	require.Error(t, err)
	_, err = testApp.anteHandler(sdk.Context{}, tx, false)
	require.ErrorIs(t, err, sdkerrors.ErrUnknownExtensionOptions)
	require.ErrorContains(t, err, "Oracle proposal option")
}

func TestFinalizeBlockAppliesOracleProposalAndPreservesRawResultIndexes(t *testing.T) {
	configureFeePolicyTestBech32Prefixes(t, true)

	validatorKey := cmtsecp256k1.GenPrivKey()
	validatorAddress := validatorKey.PubKey().Address()
	testApp := NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(oracleProposalFinalizeTestChainID),
	)
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })

	aggregator := oracleabci.NewAggregator(
		testApp.OracleKeeper,
		oracleProposalValidatorStore{
			string(validatorAddress): {
				Sum: &cmtprotocrypto.PublicKey_Secp256K1{Secp256K1: validatorKey.PubKey().Bytes()},
			},
		},
	)
	proposalHandler := oracleabci.NewProposalHandler(aggregator, nil, nil)
	testApp.OracleProposalHandler = &proposalHandler

	genesis := defaultGenesisWithConstitutionAddresses(t, testApp)
	validatorPubKey := &sdksecp256k1.PubKey{Key: validatorKey.PubKey().Bytes()}
	validatorAddressString, err := testApp.StakingKeeper.ValidatorAddressCodec().BytesToString(
		sdk.ValAddress(validatorPubKey.Address()),
	)
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(
		validatorAddressString,
		validatorPubKey,
		stakingtypes.Description{Moniker: "oracle-proposal-finalize-validator"},
	)
	require.NoError(t, err)
	bond := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = bond
	validator.DelegatorShares = sdkmath.LegacyNewDecFromInt(bond)
	delegatorAddress, err := testApp.AccountKeeper.AddressCodec().BytesToString(
		sdk.AccAddress(validatorPubKey.Address()),
	)
	require.NoError(t, err)
	stakingGenesis := stakingtypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	stakingGenesis.Validators = []stakingtypes.Validator{validator}
	stakingGenesis.Delegations = []stakingtypes.Delegation{
		stakingtypes.NewDelegation(
			delegatorAddress,
			validatorAddressString,
			sdkmath.LegacyNewDecFromInt(bond),
		),
	}
	genesis[stakingtypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(stakingGenesis)

	bankGenesis := banktypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], bankGenesis)
	bondedPoolAddress, err := testApp.AccountKeeper.AddressCodec().BytesToString(
		authtypes.NewModuleAddress(stakingtypes.BondedPoolName),
	)
	require.NoError(t, err)
	bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
		Address: bondedPoolAddress,
		Coins:   sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, bond)),
	})
	bankGenesis.Supply = nil
	genesis[banktypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(bankGenesis)

	consensusAddress, err := testApp.StakingKeeper.ConsensusAddressCodec().BytesToString(validatorAddress)
	require.NoError(t, err)
	slashingGenesis := slashingtypes.DefaultGenesisState()
	testApp.AppCodec().MustUnmarshalJSON(genesis[slashingtypes.ModuleName], slashingGenesis)
	slashingGenesis.SigningInfos = []slashingtypes.SigningInfo{{
		Address: consensusAddress,
		ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
			sdk.ConsAddress(validatorAddress),
			1,
			0,
			time.Time{},
			false,
			0,
		),
	}}
	genesis[slashingtypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(slashingGenesis)

	oracleGenesis := &oracletypes.GenesisState{}
	testApp.AppCodec().MustUnmarshalJSON(genesis[oracletypes.ModuleName], oracleGenesis)
	oracleGenesis.Tasks = []*oracletypes.OracleTask{{
		Symbol:             "BTC/USD",
		ValueType:          oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Enabled:            true,
		SubmissionInterval: 1,
	}}
	oracleGenesis.TaskSchedule = []*oracletypes.OracleTaskScheduleEntry{
		{Symbol: "BTC/USD", Height: 1},
		{Symbol: "BTC/USD", Height: 2},
	}
	genesis[oracletypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(oracleGenesis)
	require.NoError(t, testApp.ValidateChainGenesis(genesis))

	appState, err := json.Marshal(genesis)
	require.NoError(t, err)
	consensusParams := *simtestutil.DefaultConsensusParams
	consensusParams.Abci = &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 1}
	startTime := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	_, err = testApp.InitChain(&abci.RequestInitChain{
		ChainId:         oracleProposalFinalizeTestChainID,
		InitialHeight:   1,
		Time:            startTime,
		AppStateBytes:   appState,
		ConsensusParams: &consensusParams,
	})
	require.NoError(t, err)
	_, err = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   startTime.Add(time.Second),
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)

	voteExtension := []byte(nil)
	signBytes := cmttypes.VoteExtensionSignBytes(oracleProposalFinalizeTestChainID, &cmtproto.Vote{
		Extension: voteExtension,
		Height:    1,
		Round:     0,
	})
	signature, err := validatorKey.Sign(signBytes)
	require.NoError(t, err)
	signedVote := &oracletypes.OracleSignedVoteExtension{
		ValidatorAddress:   validatorAddress,
		ValidatorPower:     1,
		BlockIdFlag:        int32(cmtproto.BlockIDFlagCommit),
		VoteExtension:      voteExtension,
		ExtensionSignature: signature,
	}
	payloadTx, err := oracleabci.EncodeProposalTx(&oracletypes.OracleProposalPayload{
		Height: 2,
		VoteExtensions: &oracletypes.OracleSignedVoteExtensions{
			Round: 0,
			Votes: []*oracletypes.OracleSignedVoteExtension{signedVote},
		},
	})
	require.NoError(t, err)

	ordinaryInvalidTx := []byte("ordinary-invalid-tx")
	request := &abci.RequestFinalizeBlock{
		Height: 2,
		Time:   startTime.Add(2 * time.Second),
		Txs:    [][]byte{payloadTx, ordinaryInvalidTx},
		DecidedLastCommit: abci.CommitInfo{
			Round: 0,
			Votes: []abci.VoteInfo{{
				Validator:   abci.Validator{Address: validatorAddress, Power: 1},
				BlockIdFlag: cmtproto.BlockIDFlagCommit,
			}},
		},
	}
	response, err := testApp.FinalizeBlock(request)
	require.NoError(t, err)
	require.Len(t, response.TxResults, 2)
	require.Equal(t, uint32(0), response.TxResults[0].Code)
	require.NotEqual(t, uint32(0), response.TxResults[1].Code)
	require.Equal(t, [][]byte{payloadTx, ordinaryInvalidTx}, request.Txs)
	_, err = testApp.Commit()
	require.NoError(t, err)

	ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: 2})
	schedule, err := testApp.OracleKeeper.ListTaskSchedule(ctx)
	require.NoError(t, err)
	require.Equal(t, []*oracletypes.OracleTaskScheduleEntry{
		{Symbol: "BTC/USD", Height: 2},
		{Symbol: "BTC/USD", Height: 3},
	}, schedule)
}

type oracleProposalValidatorStore map[string]cmtprotocrypto.PublicKey

func (s oracleProposalValidatorStore) GetPubKeyByConsAddr(
	_ context.Context,
	address sdk.ConsAddress,
) (cmtprotocrypto.PublicKey, error) {
	return s[string(address)], nil
}
