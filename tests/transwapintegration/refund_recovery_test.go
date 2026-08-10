package transwapintegration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v10/modules/core/23-commitment/types"
	host "github.com/cosmos/ibc-go/v10/modules/core/24-host"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"

	guruapp "github.com/gurufinglobal/guru/v3/app"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	bextypes "github.com/gurufinglobal/guru/v3/x/bex/types"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

const (
	testChainID        = "guru-refund-integration-1"
	clientID           = "07-tendermint-0"
	connectionID       = "connection-0"
	localChannelID     = "channel-0"
	remotePortID       = "remote"
	remoteChannelID    = "channel-7"
	inputDenom         = "ufoo"
	outputDenom        = "ubar"
	inputAmount        = int64(103)
	outputAmount       = int64(101)
	outputLiquidity    = int64(1_000)
	refundClientRev    = uint64(1)
	refundClientHeight = uint64(1)

	restartChildEnv   = "GURU_TRANSWAP_RESTART_CHILD"
	restartGenesisEnv = "GURU_TRANSWAP_RESTART_GENESIS"
	restartHeightEnv  = "GURU_TRANSWAP_RESTART_HEIGHT"
)

type appScenario struct {
	t              *testing.T
	app            *guruapp.App
	ctx            sdk.Context
	clientHeight   clienttypes.Height
	exchangeID     uint64
	user           sdk.AccAddress
	userString     string
	inputTrace     transwaptypes.Denom
	inputIBCDenom  string
	outputTrace    transwaptypes.Denom
	outputIBCDenom string
}

// TestFreshGenesisAppRefundRecoveryAccounting deliberately keeps every flow in
// one top-level test. Cosmos EVM's VM activation registry is process-global and
// rejects initialization of a second full Guru App in the same test binary.
func TestFreshGenesisAppRefundRecoveryAccounting(t *testing.T) {
	configureGuruBech32Prefixes(t)
	if os.Getenv(restartChildEnv) == "1" {
		runFreshGenesisImportChild(t)
		return
	}
	s := newFreshGenesisScenario(t)

	params := transwaptypes.DefaultParams()
	params.MaxRefundRetries = 3
	require.NoError(t, s.app.TranswapKeeper.SetParams(s.ctx, params))

	// Flow 1: the original output times out, the first refund attempt times
	// out, and a freshly timed second attempt completes by acknowledgement.
	first := s.receiveSwap()
	s.assertPending(first)
	s.requireVolumeAmount(outputAmount)
	originalTimeout := first.GetOriginalTimeoutTimestamp()
	originalHeight := first.GetOriginalTimeoutHeight()

	firstAttempt, originalPacket := s.timeoutOriginalOutput(first)
	s.requireVolumeAmount(0)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_IN_FLIGHT, firstAttempt.GetStatus())
	require.Equal(t, uint32(1), firstAttempt.GetRetryCount())
	require.Equal(t, originalTimeout, firstAttempt.GetOriginalTimeoutTimestamp())
	require.Equal(t, originalHeight, firstAttempt.GetOriginalTimeoutHeight())
	require.Greater(t, firstAttempt.GetActiveTimeoutTimestamp(), originalTimeout)
	s.requireAccounting(firstAttempt, sdk.NewCoins(sdk.NewInt64Coin(s.inputIBCDenom, inputAmount)), sdk.NewCoins())
	s.requireReserveBalance(s.outputIBCDenom, outputLiquidity)
	s.requirePacketCommitted(s.activePacket(firstAttempt))
	require.Empty(t, s.packetCommitment(originalPacket))
	require.NoError(t, s.app.TranswapKeeper.AssertRefundInvariants(s.ctx))

	firstAttemptPacket := s.activePacket(firstAttempt)
	secondAttempt := s.timeoutActive(firstAttemptPacket)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_IN_FLIGHT, secondAttempt.GetStatus())
	require.Equal(t, uint32(2), secondAttempt.GetRetryCount())
	require.NotEqual(t, firstAttempt.GetActivePacketSequence(), secondAttempt.GetActivePacketSequence())
	require.Greater(t, secondAttempt.GetActiveTimeoutTimestamp(), firstAttempt.GetActiveTimeoutTimestamp())
	require.Equal(t, originalTimeout, secondAttempt.GetOriginalTimeoutTimestamp())
	require.Equal(t, originalHeight, secondAttempt.GetOriginalTimeoutHeight())
	require.Equal(
		t,
		uint64(s.ctx.BlockTime().Add(transwaptypes.DefaultRefundTimeoutWindow).UnixNano()), //nolint:gosec // fixed positive test time.
		secondAttempt.GetActiveTimeoutTimestamp(),
	)
	secondAttemptPacket := s.activePacket(secondAttempt)
	require.Empty(t, s.packetCommitment(firstAttemptPacket))
	s.requirePacketCommitted(secondAttemptPacket)
	require.NoError(t, s.app.TranswapKeeper.AssertRefundInvariants(s.ctx))

	// A stale callback cannot replace or settle the one current active packet.
	require.NoError(t, s.app.TranswapKeeper.OnTimeoutTransferPacket(
		s.ctx,
		firstAttemptPacket.SourcePort,
		firstAttemptPacket.SourceChannel,
		firstAttemptPacket.Sequence,
		s.packetData(firstAttemptPacket),
	))
	unchanged := s.mustRefund(secondAttempt.GetId())
	require.Equal(t, secondAttempt.GetActivePacketSequence(), unchanged.GetActivePacketSequence())
	require.Equal(t, secondAttempt.GetActiveTimeoutTimestamp(), unchanged.GetActiveTimeoutTimestamp())
	require.Equal(t, secondAttempt.GetRetryCount(), unchanged.GetRetryCount())

	s.acknowledgeSuccess(secondAttemptPacket)
	_, found, err := s.app.TranswapKeeper.GetRefundRecord(s.ctx, secondAttempt.GetId())
	require.NoError(t, err)
	require.False(t, found, "completed refund transport record must be pruned")
	s.requireAccounting(secondAttempt, sdk.NewCoins(), sdk.NewCoins())
	s.requireReserveBalance(s.inputIBCDenom, 0)
	s.requireReserveBalance(s.outputIBCDenom, outputLiquidity)
	require.NoError(t, s.app.TranswapKeeper.AssertRefundInvariants(s.ctx))
	require.NoError(t, s.app.BexKeeper.AssertInvariants(s.ctx))

	// Duplicate ACK is an idempotent no-op and a completed refund cannot be
	// converted into a local claim.
	userBeforeDuplicateAck := s.app.BankKeeper.GetAllBalances(s.ctx, s.user)
	require.NoError(t, s.app.TranswapKeeper.OnAcknowledgementTransferPacket(
		s.ctx,
		secondAttemptPacket.SourcePort,
		secondAttemptPacket.SourceChannel,
		secondAttemptPacket.Sequence,
		s.packetData(secondAttemptPacket),
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
	require.Equal(t, userBeforeDuplicateAck, s.app.BankKeeper.GetAllBalances(s.ctx, s.user))
	_, err = s.app.TranswapKeeper.ClaimRefund(s.ctx, secondAttempt.GetId(), s.userString)
	require.ErrorIs(t, err, transwaptypes.ErrRefundNotFound)

	// Flow 2: one bounded attempt is exhausted, leaving reserve-backed funds
	// for the receiver's idempotent manual claim.
	params.MaxRefundRetries = 1
	require.NoError(t, s.app.TranswapKeeper.SetParams(s.ctx, params))
	s.refreshOracle()
	second := s.receiveSwap()
	s.assertPending(second)
	s.requireVolumeAmount(outputAmount)
	manualAttempt, _ := s.timeoutOriginalOutput(second)
	s.requireVolumeAmount(0)
	require.Equal(t, uint32(1), manualAttempt.GetRetryCount())
	manualPacket := s.activePacket(manualAttempt)
	claimable := s.timeoutActive(manualPacket)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE, claimable.GetStatus())
	require.Equal(t, uint32(1), claimable.GetRetryCount())
	require.Zero(t, claimable.GetActivePacketSequence())
	require.Zero(t, claimable.GetActiveTimeoutTimestamp())
	s.requireAccounting(claimable, sdk.NewCoins(sdk.NewInt64Coin(s.inputIBCDenom, inputAmount)), sdk.NewCoins())
	require.Equal(
		t,
		sdk.NewInt64Coin(s.inputIBCDenom, inputAmount).Amount,
		s.app.BankKeeper.GetBalance(s.ctx, s.reserveAddress(), s.inputIBCDenom).Amount,
	)
	require.NoError(t, s.app.TranswapKeeper.AssertRefundInvariants(s.ctx))

	userBeforeClaim := s.app.BankKeeper.GetBalance(s.ctx, s.user, s.inputIBCDenom)
	claimed, err := s.app.TranswapKeeper.ClaimRefund(s.ctx, claimable.GetId(), s.userString)
	require.NoError(t, err)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_CLAIMED, claimed.GetStatus())
	userAfterClaim := s.app.BankKeeper.GetBalance(s.ctx, s.user, s.inputIBCDenom)
	require.Equal(t, userBeforeClaim.Amount.AddRaw(inputAmount), userAfterClaim.Amount)
	s.requireAccounting(claimed, sdk.NewCoins(), sdk.NewCoins())
	s.requireReserveBalance(s.inputIBCDenom, 0)
	s.requireReserveBalance(s.outputIBCDenom, outputLiquidity)

	claimedAgain, err := s.app.TranswapKeeper.ClaimRefund(s.ctx, claimable.GetId(), s.userString)
	require.NoError(t, err)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_CLAIMED, claimedAgain.GetStatus())
	require.Equal(t, userAfterClaim.Amount, s.app.BankKeeper.GetBalance(s.ctx, s.user, s.inputIBCDenom).Amount)

	// The packet already timed out before claimability. A late or duplicate
	// success callback therefore has no active index and cannot pay twice.
	require.NoError(t, s.app.TranswapKeeper.OnAcknowledgementTransferPacket(
		s.ctx,
		manualPacket.SourcePort,
		manualPacket.SourceChannel,
		manualPacket.Sequence,
		s.packetData(manualPacket),
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_CLAIMED, s.mustRefund(claimable.GetId()).GetStatus())
	require.Equal(t, userAfterClaim.Amount, s.app.BankKeeper.GetBalance(s.ctx, s.user, s.inputIBCDenom).Amount)
	require.NoError(t, s.app.TranswapKeeper.AssertRefundInvariants(s.ctx))
	require.NoError(t, s.app.BexKeeper.AssertInvariants(s.ctx))

	// Export every module changed by this scenario into a complete fresh-genesis
	// baseline, then import it into a fresh Guru App and MemDB in an isolated
	// child process. Process isolation is required because Cosmos EVM's opcode
	// activator registry is intentionally global.
	bexGenesis, err := s.app.BexKeeper.ExportGenesis(s.ctx)
	require.NoError(t, err)
	transwapGenesis := s.app.TranswapKeeper.ExportGenesis(s.ctx)
	require.NoError(t, transwaptypes.ValidateGenesisState(transwapGenesis))
	require.NotNil(t, bexGenesis)
	runFreshGenesisImport(t, s.exportRoundTripGenesis(), 1)

	// Flow 3: a successful original output keeps the charged volume but prunes
	// all refund state after releasing fee and liability accounting.
	s.refreshOracle()
	successful := s.receiveSwap()
	s.assertPending(successful)
	s.requireVolumeAmount(outputAmount)
	successPacket := s.originalOutputPacket(successful)
	s.acknowledgeSuccess(successPacket)
	_, found, err = s.app.TranswapKeeper.GetRefundRecord(s.ctx, successful.GetId())
	require.NoError(t, err)
	require.False(t, found)
	s.requireVolumeAmount(outputAmount)
	s.requireAccounting(successful, sdk.NewCoins(), sdk.NewCoins())
	require.NoError(t, s.app.TranswapKeeper.OnAcknowledgementTransferPacket(
		s.ctx,
		successPacket.SourcePort,
		successPacket.SourceChannel,
		successPacket.Sequence,
		s.packetData(successPacket),
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
	s.requireVolumeAmount(outputAmount)
}

func (s *appScenario) exportRoundTripGenesis() json.RawMessage {
	s.t.Helper()

	genesis := s.app.BuildChainDefaultGenesis()
	constitutionGenesis := &constitutionv1.GenesisState{}
	s.app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = appAddress(s.t, s.app, 0x41)
	constitutionGenesis.ModeratorAddress = appAddress(s.t, s.app, 0x42)
	genesis[constitutiontypes.ModuleName] = s.app.AppCodec().MustMarshalJSON(constitutionGenesis)
	configureGenesisValidator(s.t, s.app, genesis)

	changed, err := s.app.ModuleManager.ExportGenesisForModules(
		s.ctx,
		s.app.AppCodec(),
		[]string{
			banktypes.ModuleName,
			ibcexported.ModuleName,
			oracletypes.ModuleName,
			bextypes.ModuleName,
			transwaptypes.ModuleName,
		},
	)
	require.NoError(s.t, err)
	for moduleName, state := range changed {
		genesis[moduleName] = state
	}
	appState, err := json.Marshal(genesis)
	require.NoError(s.t, err)
	return appState
}

func runFreshGenesisImport(t *testing.T, appState json.RawMessage, initialHeight int64) {
	t.Helper()

	genesisPath := filepath.Join(t.TempDir(), "exported-genesis.json")
	require.NoError(t, os.WriteFile(genesisPath, appState, 0o600))
	cmd := exec.Command( //nolint:gosec // executes this already-built test binary with fixed arguments.
		os.Args[0],
		"-test.run=^TestFreshGenesisAppRefundRecoveryAccounting$",
		"-test.count=1",
	)
	cmd.Env = append(
		os.Environ(),
		restartChildEnv+"=1",
		restartGenesisEnv+"="+genesisPath,
		restartHeightEnv+"="+strconv.FormatInt(initialHeight, 10),
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "fresh-App genesis import failed:\n%s", output)
}

func runFreshGenesisImportChild(t *testing.T) {
	t.Helper()

	genesisPath := os.Getenv(restartGenesisEnv)
	require.NotEmpty(t, genesisPath)
	appState, err := os.ReadFile(genesisPath)
	require.NoError(t, err)
	initialHeight, err := strconv.ParseInt(os.Getenv(restartHeightEnv), 10, 64)
	require.NoError(t, err)
	require.Positive(t, initialHeight)

	var genesis map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appState, &genesis))
	application := guruapp.NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.AppOptionsMap{
			"oracle.enabled":         false,
			server.FlagMempoolMaxTxs: -1,
		},
		baseapp.SetChainID(testChainID),
	)
	t.Cleanup(func() { require.NoError(t, application.Close()) })

	importTime := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	_, err = application.InitChain(&types.RequestInitChain{
		ChainId:         testChainID,
		InitialHeight:   initialHeight,
		Time:            importTime,
		AppStateBytes:   appState,
		ConsensusParams: simtestutil.DefaultConsensusParams,
	})
	require.NoError(t, err)
	ctx := application.NewContextLegacy(false, cmtproto.Header{
		ChainID: testChainID,
		Height:  initialHeight,
		Time:    importTime,
	})

	records := application.TranswapKeeper.GetAllRefundRecords(ctx)
	require.Len(t, records, 1)
	statuses := make(map[transwaptypes.RefundStatus]int, 1)
	for _, record := range records {
		statuses[record.GetStatus()]++
		require.NotZero(t, record.GetOriginalTimeoutTimestamp())
		require.NotNil(t, record.GetOriginalTimeoutHeight())
		require.Zero(t, record.GetActivePacketSequence())
		require.Zero(t, record.GetActiveTimeoutTimestamp())
	}
	require.Equal(t, 1, statuses[transwaptypes.RefundStatus_REFUND_STATUS_CLAIMED])
	params, err := application.TranswapKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(1), params.GetMaxRefundRetries())
	require.NoError(t, application.TranswapKeeper.AssertRefundInvariants(ctx))
	require.NoError(t, application.BexKeeper.AssertInvariants(ctx))

	inputTrace := transwaptypes.NewDenom(inputDenom, transwaptypes.NewHop(transwaptypes.PortID, localChannelID))
	inputIBCDenom := transwaptypes.DenomIBCDenom(inputTrace)
	outputTrace := transwaptypes.NewDenom(outputDenom, transwaptypes.NewHop(transwaptypes.PortID, localChannelID))
	outputIBCDenom := transwaptypes.DenomIBCDenom(outputTrace)
	user := sdk.AccAddress(bytes.Repeat([]byte{0x51}, 20))
	reserve := application.BexKeeper.GetReserveAddress(ctx, 1)
	require.Equal(t, sdk.NewInt64Coin(inputIBCDenom, inputAmount).Amount, application.BankKeeper.GetBalance(ctx, user, inputIBCDenom).Amount)
	require.True(t, application.BankKeeper.GetBalance(ctx, reserve, inputIBCDenom).IsZero())
	require.Equal(t, sdk.NewInt64Coin(outputIBCDenom, outputLiquidity).Amount, application.BankKeeper.GetBalance(ctx, reserve, outputIBCDenom).Amount)

	channel, found := application.IBCKeeper.ChannelKeeper.GetChannel(ctx, transwaptypes.PortID, localChannelID)
	require.True(t, found)
	require.Equal(t, channeltypes.OPEN, channel.State)
	nextSequence, found := application.IBCKeeper.ChannelKeeper.GetNextSequenceSend(ctx, transwaptypes.PortID, localChannelID)
	require.True(t, found)
	require.Equal(t, uint64(6), nextSequence)
	for sequence := uint64(1); sequence < nextSequence; sequence++ {
		require.Empty(t, application.IBCKeeper.ChannelKeeper.GetPacketCommitment(ctx, transwaptypes.PortID, localChannelID, sequence))
	}

	actualModules, err := application.ModuleManager.ExportGenesisForModules(
		ctx,
		application.AppCodec(),
		[]string{banktypes.ModuleName},
	)
	require.NoError(t, err)
	expectedBank := banktypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], expectedBank)
	actualBank := banktypes.DefaultGenesisState()
	application.AppCodec().MustUnmarshalJSON(actualModules[banktypes.ModuleName], actualBank)
	require.Equal(t, expectedBank, actualBank, "bank genesis changed across fresh import/export")

	expectedBex := &bextypes.GenesisState{}
	application.AppCodec().MustUnmarshalJSON(genesis[bextypes.ModuleName], expectedBex)
	actualBex, err := application.BexKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	requireEqualGogoMessage(t, expectedBex, actualBex)
	expectedTranswap := &transwaptypes.GenesisState{}
	application.AppCodec().MustUnmarshalJSON(genesis[transwaptypes.ModuleName], expectedTranswap)
	actualTranswap := application.TranswapKeeper.ExportGenesis(ctx)
	requireEqualGogoMessage(t, expectedTranswap, actualTranswap)
}

type gogoMarshaler interface {
	Marshal() ([]byte, error)
}

func requireEqualGogoMessage(t *testing.T, expected, actual gogoMarshaler) {
	t.Helper()
	expectedBytes, err := expected.Marshal()
	require.NoError(t, err)
	actualBytes, err := actual.Marshal()
	require.NoError(t, err)
	require.Equal(t, expectedBytes, actualBytes)
}

func newFreshGenesisScenario(t *testing.T) *appScenario {
	t.Helper()

	application := guruapp.NewApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.AppOptionsMap{
			"oracle.enabled":         false,
			server.FlagMempoolMaxTxs: -1,
		},
		baseapp.SetChainID(testChainID),
	)
	t.Cleanup(func() { require.NoError(t, application.Close()) })

	genesis := application.BuildChainDefaultGenesis()
	constitutionGenesis := &constitutionv1.GenesisState{}
	application.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
	constitutionGenesis.BaseAddress = appAddress(t, application, 0x41)
	constitutionGenesis.ModeratorAddress = appAddress(t, application, 0x42)
	genesis[constitutiontypes.ModuleName] = application.AppCodec().MustMarshalJSON(constitutionGenesis)
	configureGenesisValidator(t, application, genesis)
	stateBytes, err := json.Marshal(genesis)
	require.NoError(t, err)

	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	_, err = application.InitChain(&types.RequestInitChain{
		ChainId:         testChainID,
		InitialHeight:   1,
		Time:            start,
		AppStateBytes:   stateBytes,
		ConsensusParams: simtestutil.DefaultConsensusParams,
	})
	require.NoError(t, err)
	ctx := application.NewContextLegacy(false, cmtproto.Header{
		ChainID: testChainID,
		Height:  1,
		Time:    start,
	})

	s := &appScenario{
		t:            t,
		app:          application,
		ctx:          ctx,
		clientHeight: clienttypes.NewHeight(refundClientRev, refundClientHeight),
		user:         sdk.AccAddress(bytes.Repeat([]byte{0x51}, 20)),
		inputTrace:   transwaptypes.NewDenom(inputDenom, transwaptypes.NewHop(transwaptypes.PortID, localChannelID)),
		outputTrace:  transwaptypes.NewDenom(outputDenom, transwaptypes.NewHop(transwaptypes.PortID, localChannelID)),
	}
	s.userString, err = application.AccountKeeper.AddressCodec().BytesToString(s.user)
	require.NoError(t, err)
	s.inputIBCDenom = transwaptypes.DenomIBCDenom(s.inputTrace)
	s.outputIBCDenom = transwaptypes.DenomIBCDenom(s.outputTrace)

	s.configureIBCClientAndChannel(start)
	s.configureExchangeAndLiquidity()
	return s
}

func (s *appScenario) configureIBCClientAndChannel(timestamp time.Time) {
	s.t.Helper()

	clientState := ibctm.NewClientState(
		"remote-1",
		ibctm.DefaultTrustLevel,
		24*time.Hour,
		48*time.Hour,
		time.Minute,
		s.clientHeight,
		commitmenttypes.GetSDKSpecs(),
		[]string{"upgrade", "upgradedIBCState"},
	)
	s.app.IBCKeeper.ClientKeeper.SetClientState(s.ctx, clientID, clientState)
	s.setClientConsensusTimestamp(timestamp)

	connection := connectiontypes.NewConnectionEnd(
		connectiontypes.OPEN,
		clientID,
		connectiontypes.NewCounterparty(
			"07-tendermint-1",
			"connection-1",
			commitmenttypes.NewMerklePrefix([]byte(ibcexported.StoreKey)),
		),
		connectiontypes.GetCompatibleVersions(),
		0,
	)
	s.app.IBCKeeper.ConnectionKeeper.SetConnection(s.ctx, connectionID, connection)

	channel := channeltypes.NewChannel(
		channeltypes.OPEN,
		channeltypes.UNORDERED,
		channeltypes.NewCounterparty(remotePortID, remoteChannelID),
		[]string{connectionID},
		transwaptypes.V1,
	)
	s.app.IBCKeeper.ChannelKeeper.SetChannel(s.ctx, transwaptypes.PortID, localChannelID, channel)
	s.app.IBCKeeper.ChannelKeeper.SetNextSequenceSend(s.ctx, transwaptypes.PortID, localChannelID, 1)
	require.Equal(s.t, ibcexported.Active, s.app.IBCKeeper.ClientKeeper.GetClientStatus(s.ctx, clientID))
}

func (s *appScenario) configureExchangeAndLiquidity() {
	s.t.Helper()

	moderator, err := s.app.ConstitutionKeeper.GetModeratorAddress(s.ctx)
	require.NoError(s.t, err)
	admin := appAddress(s.t, s.app, 0x52)
	require.NoError(s.t, s.app.BexKeeper.RegisterAdmin(s.ctx, moderator, admin))

	exchange, err := s.app.BexKeeper.RegisterExchange(s.ctx, &bextypes.MsgRegisterExchange{
		BexAdminAddress:           admin,
		ExchangeAdminAddress:      admin,
		DenomA:                    inputDenom,
		PortA:                     transwaptypes.PortID,
		ChannelA:                  localChannelID,
		DenomB:                    outputDenom,
		PortB:                     transwaptypes.PortID,
		ChannelB:                  localChannelID,
		OracleSymbolAToB:          "FOO/BAR",
		OracleSymbolBToA:          "BAR/FOO",
		FeeBpsAToB:                100,
		FeeBpsBToA:                100,
		LimitAToB:                 "1000000",
		LimitBToA:                 "1000000",
		VolumeCapAToB:             "1000000",
		VolumeCapBToA:             "1000000",
		Status:                    bextypes.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		Metadata:                  map[string]string{"test": "fresh-genesis-refund-recovery"},
		VolumeEpochSeconds:        86_400,
		MaxOracleStalenessSeconds: 3_600,
	})
	require.NoError(s.t, err)
	s.exchangeID = exchange.GetId()
	require.Equal(s.t, s.inputIBCDenom, exchange.GetIbcDenomA())
	require.Equal(s.t, s.outputIBCDenom, exchange.GetIbcDenomB())

	s.app.TranswapKeeper.SetDenom(s.ctx, s.outputTrace)
	s.app.TranswapKeeper.SetDenomMetadata(s.ctx, s.outputTrace)
	s.refreshOracle()

	liquidity := sdk.NewInt64Coin(s.outputIBCDenom, outputLiquidity)
	require.NoError(s.t, s.app.BankKeeper.MintCoins(s.ctx, transwaptypes.ModuleName, sdk.NewCoins(liquidity)))
	require.NoError(s.t, s.app.BexKeeper.ReceiveToReserve(
		s.ctx,
		s.exchangeID,
		s.app.AccountKeeper.GetModuleAddress(transwaptypes.ModuleName),
		sdk.NewCoins(liquidity),
	))
	require.NoError(s.t, s.app.BexKeeper.AssertInvariants(s.ctx))
}

func (s *appScenario) receiveSwap() *transwaptypes.RefundRecord {
	s.t.Helper()

	before, found := s.app.IBCKeeper.ChannelKeeper.GetNextSequenceSend(s.ctx, transwaptypes.PortID, localChannelID)
	require.True(s.t, found)
	originalTimeout := uint64(s.ctx.BlockTime().Add(2 * time.Minute).UnixNano()) //nolint:gosec // fixed positive test time.
	originalHeight := clienttypes.NewHeight(7, 42)
	data := transwaptypes.NewInternalTransferRepresentation(
		strconv.FormatUint(s.exchangeID, 10),
		&transwaptypes.Token{Denom: transwaptypes.NewDenom(inputDenom), Amount: strconv.FormatInt(inputAmount, 10)},
		s.userString,
		s.userString,
		`guru.transwap.protection:v1:{"min_amount_out":"101","expected_exchange_revision":"1"}`,
	)
	require.NoError(s.t, s.app.TranswapKeeper.OnRecvExchangePacket(
		s.ctx,
		data,
		remotePortID,
		remoteChannelID,
		transwaptypes.PortID,
		localChannelID,
		originalTimeout,
		originalHeight,
	))

	after, found := s.app.IBCKeeper.ChannelKeeper.GetNextSequenceSend(s.ctx, transwaptypes.PortID, localChannelID)
	require.True(s.t, found)
	require.Equal(s.t, before+1, after)
	refundID := transwaptypes.RefundID(transwaptypes.PortID, localChannelID, before)
	return s.mustRefund(refundID)
}

func (s *appScenario) assertPending(record *transwaptypes.RefundRecord) {
	s.t.Helper()

	require.Equal(s.t, transwaptypes.RefundStatus_REFUND_STATUS_PENDING, record.GetStatus())
	require.Equal(s.t, uint32(0), record.GetRetryCount())
	require.Equal(s.t, uint64(7), record.GetOriginalTimeoutHeight().GetRevisionNumber())
	require.Equal(s.t, uint64(42), record.GetOriginalTimeoutHeight().GetRevisionHeight())
	originalPacket := s.originalOutputPacket(record)
	require.Equal(s.t, channeltypes.CommitPacket(originalPacket), record.GetOriginalOutputPacketCommitment())
	s.requirePacketCommitted(originalPacket)
	s.requireAccounting(
		record,
		sdk.NewCoins(sdk.NewInt64Coin(s.inputIBCDenom, inputAmount)),
		sdk.NewCoins(sdk.NewInt64Coin(s.inputIBCDenom, inputAmount-outputAmount)),
	)
	require.Equal(
		s.t,
		sdk.NewInt64Coin(s.inputIBCDenom, outputAmount).Amount,
		s.app.BankKeeper.GetBalance(s.ctx, s.reserveAddress(), s.inputIBCDenom).Amount,
	)
	s.requireReserveBalance(s.outputIBCDenom, outputLiquidity-outputAmount)
	require.NoError(s.t, s.app.TranswapKeeper.AssertRefundInvariants(s.ctx))
	require.NoError(s.t, s.app.BexKeeper.AssertInvariants(s.ctx))
}

func (s *appScenario) timeoutOriginalOutput(record *transwaptypes.RefundRecord) (*transwaptypes.RefundRecord, channeltypes.Packet) {
	s.t.Helper()

	packet := s.originalOutputPacket(record)
	s.timeoutPacket(packet)
	return s.mustRefund(record.GetId()), packet
}

func (s *appScenario) timeoutActive(packet channeltypes.Packet) *transwaptypes.RefundRecord {
	s.t.Helper()
	s.timeoutPacket(packet)
	records := s.app.TranswapKeeper.GetAllRefundRecords(s.ctx)
	for _, record := range records {
		if record.GetRefundSourcePort() == packet.SourcePort &&
			record.GetRefundSourceChannel() == packet.SourceChannel &&
			record.GetOriginalOutputSequence() <= packet.Sequence &&
			(record.GetActivePacketSequence() != 0 ||
				record.GetStatus() == transwaptypes.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE) {
			return record
		}
	}
	require.FailNow(s.t, "refund record not found after active timeout", "packet sequence %d", packet.Sequence)
	return nil
}

func (s *appScenario) timeoutPacket(packet channeltypes.Packet) {
	s.t.Helper()

	require.LessOrEqual(s.t, packet.TimeoutTimestamp, uint64(^uint64(0)>>1))
	s.advanceTo(time.Unix(0, int64(packet.TimeoutTimestamp)).Add(time.Second)) //nolint:gosec // bounded above.
	s.deletePacketCommitment(packet)
	require.NoError(s.t, s.app.TranswapKeeper.OnTimeoutTransferPacket(
		s.ctx,
		packet.SourcePort,
		packet.SourceChannel,
		packet.Sequence,
		s.packetData(packet),
	))
}

func (s *appScenario) acknowledgeSuccess(packet channeltypes.Packet) {
	s.t.Helper()
	s.deletePacketCommitment(packet)
	require.NoError(s.t, s.app.TranswapKeeper.OnAcknowledgementTransferPacket(
		s.ctx,
		packet.SourcePort,
		packet.SourceChannel,
		packet.Sequence,
		s.packetData(packet),
		channeltypes.NewResultAcknowledgement([]byte{1}),
	))
}

func (s *appScenario) originalOutputPacket(record *transwaptypes.RefundRecord) channeltypes.Packet {
	s.t.Helper()
	reserveString, err := s.app.AccountKeeper.AddressCodec().BytesToString(s.reserveAddress())
	require.NoError(s.t, err)
	data := transwaptypes.NewFungibleTokenPacketData(
		transwaptypes.DenomPath(s.outputTrace),
		strconv.FormatInt(outputAmount, 10),
		reserveString,
		s.userString,
		"Station exchange",
	)
	return channeltypes.NewPacket(
		transwaptypes.FungibleTokenPacketDataBytes(data),
		record.GetOriginalOutputSequence(),
		record.GetOriginalOutputPort(),
		record.GetOriginalOutputChannel(),
		remotePortID,
		remoteChannelID,
		clienttypes.ZeroHeight(),
		record.GetOriginalTimeoutTimestamp(),
	)
}

func (s *appScenario) activePacket(record *transwaptypes.RefundRecord) channeltypes.Packet {
	s.t.Helper()
	reserveString, err := s.app.AccountKeeper.AddressCodec().BytesToString(s.reserveAddress())
	require.NoError(s.t, err)
	token := record.GetToken()
	data := transwaptypes.NewFungibleTokenPacketData(
		transwaptypes.DenomPath(token.GetDenom()),
		token.GetAmount(),
		reserveString,
		record.GetReceiver(),
		record.GetMemo(),
	)
	channel, found := s.app.IBCKeeper.ChannelKeeper.GetChannel(
		s.ctx,
		record.GetRefundSourcePort(),
		record.GetRefundSourceChannel(),
	)
	require.True(s.t, found)
	return channeltypes.NewPacket(
		transwaptypes.FungibleTokenPacketDataBytes(data),
		record.GetActivePacketSequence(),
		record.GetRefundSourcePort(),
		record.GetRefundSourceChannel(),
		channel.Counterparty.PortId,
		channel.Counterparty.ChannelId,
		clienttypes.ZeroHeight(),
		record.GetActiveTimeoutTimestamp(),
	)
}

func (s *appScenario) packetData(packet channeltypes.Packet) transwaptypes.InternalTransferRepresentation {
	s.t.Helper()
	data, err := transwaptypes.UnmarshalPacketData(packet.Data, transwaptypes.V1, "")
	require.NoError(s.t, err)
	return data
}

func (s *appScenario) mustRefund(refundID string) *transwaptypes.RefundRecord {
	s.t.Helper()
	record, found, err := s.app.TranswapKeeper.GetRefundRecord(s.ctx, refundID)
	require.NoError(s.t, err)
	require.True(s.t, found)
	return record
}

func (s *appScenario) reserveAddress() sdk.AccAddress {
	return s.app.BexKeeper.GetReserveAddress(s.ctx, s.exchangeID)
}

func (s *appScenario) refreshOracle() {
	s.t.Helper()
	require.NoError(s.t, s.app.OracleKeeper.SetLatestValue(s.ctx, &oracletypes.OracleValue{
		Symbol:        "FOO/BAR",
		ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Value:         "1",
		BlockHeight:   s.ctx.BlockHeight(),
		BlockTimeUnix: s.ctx.BlockTime().Unix(),
	}))
}

func (s *appScenario) advanceTo(timestamp time.Time) {
	s.ctx = s.ctx.WithBlockTime(timestamp)
	s.setClientConsensusTimestamp(timestamp)
	require.Equal(s.t, ibcexported.Active, s.app.IBCKeeper.ClientKeeper.GetClientStatus(s.ctx, clientID))
}

func (s *appScenario) setClientConsensusTimestamp(timestamp time.Time) {
	consensus := ibctm.NewConsensusState(
		timestamp,
		commitmenttypes.NewMerkleRoot(bytes.Repeat([]byte{0x61}, 32)),
		bytes.Repeat([]byte{0x62}, 32),
	)
	s.app.IBCKeeper.ClientKeeper.SetClientConsensusState(s.ctx, clientID, s.clientHeight, consensus)
}

func (s *appScenario) packetCommitment(packet channeltypes.Packet) []byte {
	return s.app.IBCKeeper.ChannelKeeper.GetPacketCommitment(
		s.ctx,
		packet.SourcePort,
		packet.SourceChannel,
		packet.Sequence,
	)
}

func (s *appScenario) requirePacketCommitted(packet channeltypes.Packet) {
	s.t.Helper()
	require.Equal(s.t, channeltypes.CommitPacket(packet), s.packetCommitment(packet))
}

// IBC core consumes the source commitment before invoking an application
// timeout/acknowledgement callback. Proof verification is outside this test's
// one-App boundary; deleting the real IBC store key preserves callback order.
func (s *appScenario) deletePacketCommitment(packet channeltypes.Packet) {
	s.t.Helper()
	store := runtime.NewKVStoreService(s.app.GetKVStoreKey(ibcexported.StoreKey)).OpenKVStore(s.ctx)
	require.NoError(s.t, store.Delete(host.PacketCommitmentKey(
		packet.SourcePort,
		packet.SourceChannel,
		packet.Sequence,
	)))
	require.Empty(s.t, s.packetCommitment(packet))
}

func (s *appScenario) requireAccounting(record *transwaptypes.RefundRecord, pending, locked sdk.Coins) {
	s.t.Helper()
	actualPending, err := s.app.BexKeeper.GetPendingLiabilities(s.ctx, s.exchangeID)
	require.NoError(s.t, err)
	require.Equal(s.t, pending, actualPending, "refund %s pending liability", record.GetId())
	actualLocked, err := s.app.BexKeeper.GetLockedFees(s.ctx, s.exchangeID)
	require.NoError(s.t, err)
	require.Equal(s.t, locked, actualLocked, "refund %s locked fees", record.GetId())
}

func (s *appScenario) requireReserveBalance(denom string, amount int64) {
	s.t.Helper()
	require.Equal(
		s.t,
		sdk.NewInt64Coin(denom, amount).Amount,
		s.app.BankKeeper.GetBalance(s.ctx, s.reserveAddress(), denom).Amount,
	)
}

func (s *appScenario) requireVolumeAmount(amount int64) {
	s.t.Helper()
	exchange, err := s.app.BexKeeper.GetExchange(s.ctx, s.exchangeID)
	require.NoError(s.t, err)
	used, err := s.app.BexKeeper.GetCurrentVolumeAmount(
		s.ctx,
		exchange,
		bextypes.SwapDirection_SWAP_DIRECTION_A_TO_B,
	)
	require.NoError(s.t, err)
	require.Equal(s.t, sdkmath.NewInt(amount), used)
}

func appAddress(t *testing.T, app *guruapp.App, fill byte) string {
	t.Helper()
	address, err := app.AccountKeeper.AddressCodec().BytesToString(bytes.Repeat([]byte{fill}, 20))
	require.NoError(t, err)
	return address
}

func configureGenesisValidator(t *testing.T, app *guruapp.App, genesis map[string]json.RawMessage) {
	t.Helper()

	bond := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	pubKey := simtestutil.CreateTestPubKeys(1)[0]
	validatorBytes := sdk.ValAddress(pubKey.Address().Bytes())
	validatorAddress, err := app.StakingKeeper.ValidatorAddressCodec().BytesToString(validatorBytes)
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(
		validatorAddress,
		pubKey,
		stakingtypes.Description{Moniker: "integration-validator"},
	)
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = bond
	validator.DelegatorShares = sdkmath.LegacyOneDec()

	delegatorAddress, err := app.AccountKeeper.AddressCodec().BytesToString(sdk.AccAddress(validatorBytes))
	require.NoError(t, err)
	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	stakingGenesis.Validators = []stakingtypes.Validator{validator}
	stakingGenesis.Delegations = []stakingtypes.Delegation{
		stakingtypes.NewDelegation(delegatorAddress, validatorAddress, sdkmath.LegacyOneDec()),
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)

	bankGenesis := banktypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], bankGenesis)
	bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(stakingGenesis.Params.BondDenom, bond)),
	})
	genesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
}

func configureGuruBech32Prefixes(t *testing.T) {
	t.Helper()
	cfg := sdk.GetConfig()
	accountAddr := cfg.GetBech32AccountAddrPrefix()
	accountPub := cfg.GetBech32AccountPubPrefix()
	validatorAddr := cfg.GetBech32ValidatorAddrPrefix()
	validatorPub := cfg.GetBech32ValidatorPubPrefix()
	consensusAddr := cfg.GetBech32ConsensusAddrPrefix()
	consensusPub := cfg.GetBech32ConsensusPubPrefix()

	cfg.SetBech32PrefixForAccount(appparams.Bech32PrefixAccAddr, appparams.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(appparams.Bech32PrefixValAddr, appparams.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(appparams.Bech32PrefixConsAddr, appparams.Bech32PrefixConsPub)
	t.Cleanup(func() {
		cfg.SetBech32PrefixForAccount(accountAddr, accountPub)
		cfg.SetBech32PrefixForValidator(validatorAddr, validatorPub)
		cfg.SetBech32PrefixForConsensusNode(consensusAddr, consensusPub)
	})
}
