//go:build test

// This acceptance harness needs two full Guru Apps in one process. Cosmos EVM
// exposes its process-global VM reset hooks only with the `test` build tag.
package transwaptwochain_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	constitutionv1 "github.com/gurufinglobal/guru/v3/api/guru/constitution/v1"
	bexv1 "github.com/gurufinglobal/guru/v3/x/bex/types"

	guruapp "github.com/gurufinglobal/guru/v3/app"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	constitutiontypes "github.com/gurufinglobal/guru/v3/x/constitution/types"
	transwaptypes "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
)

const (
	inputDenom       = "uinput"
	outputDenom      = "ubar"
	inputAmount      = int64(103)
	outputAmount     = int64(101)
	outputLiquidity  = int64(1_000)
	refundTestWindow = 10 * time.Minute
)

type testingGuruApp struct {
	*guruapp.App
	bankMetadata    []banktypes.Metadata
	bankSendEnabled []banktypes.SendEnabled
}

func (app testingGuruApp) GetBaseApp() *baseapp.BaseApp    { return app.BaseApp }
func (app testingGuruApp) GetIBCKeeper() *ibckeeper.Keeper { return app.IBCKeeper }
func (app testingGuruApp) GetTxConfig() client.TxConfig    { return app.TxConfig() }

func (app testingGuruApp) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	// ibc-go's generic setup replaces bank metadata and delegates every
	// validator from one unrelated account. Restore Guru's required EVM denom
	// metadata and constitution-compliant validator self-bonds before InitChain.
	genesis := map[string]json.RawMessage{}
	if err := json.Unmarshal(req.AppStateBytes, &genesis); err != nil {
		return nil, err
	}
	bankGenesis := banktypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], bankGenesis)
	bankGenesis.DenomMetadata = app.bankMetadata
	bankGenesis.SendEnabled = app.bankSendEnabled
	genesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
	stakingGenesis := stakingtypes.DefaultGenesisState()
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], stakingGenesis)
	stakingGenesis.Delegations = make([]stakingtypes.Delegation, 0, len(stakingGenesis.Validators))
	for _, validator := range stakingGenesis.Validators {
		validatorBytes, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.OperatorAddress)
		if err != nil {
			return nil, err
		}
		delegator, err := app.AccountKeeper.AddressCodec().BytesToString(validatorBytes)
		if err != nil {
			return nil, err
		}
		stakingGenesis.Delegations = append(
			stakingGenesis.Delegations,
			stakingtypes.NewDelegation(delegator, validator.OperatorAddress, validator.DelegatorShares),
		)
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
	appState, err := json.Marshal(genesis)
	if err != nil {
		return nil, err
	}
	request := *req
	request.AppStateBytes = appState
	return app.App.InitChain(&request)
}

func TestProofRelayedRefundTimeoutRetryAndAcknowledgement(t *testing.T) {
	configureGuruBech32Prefixes(t)
	t.Cleanup(func() { evmtypes.NewEVMConfigurator().ResetTestConfig() })
	created := 0
	creator := func() (ibctesting.TestingApp, map[string]json.RawMessage) {
		if created > 0 {
			// The two Apps use identical EVM configuration, but each VM module
			// registers it from its own InitGenesis sync.Once. Clear the test-only
			// process globals before constructing the second App.
			evmtypes.NewEVMConfigurator().ResetTestConfig()
		}
		created++
		app := guruapp.NewApp(
			log.NewNopLogger(),
			db.NewMemDB(),
			true,
			simtestutil.AppOptionsMap{
				"oracle.enabled":         false,
				server.FlagMempoolMaxTxs: -1,
			},
		)
		genesis := app.BuildChainDefaultGenesis()
		bankGenesis := banktypes.DefaultGenesisState()
		app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], bankGenesis)
		constitutionGenesis := &constitutionv1.GenesisState{}
		app.AppCodec().MustUnmarshalJSON(genesis[constitutiontypes.ModuleName], constitutionGenesis)
		constitutionGenesis.BaseAddress = mustAppAddress(t, app, 0x41)
		constitutionGenesis.ModeratorAddress = mustAppAddress(t, app, 0x42)
		genesis[constitutiontypes.ModuleName] = app.AppCodec().MustMarshalJSON(constitutionGenesis)
		return testingGuruApp{
			App:             app,
			bankMetadata:    bankGenesis.DenomMetadata,
			bankSendEnabled: bankGenesis.SendEnabled,
		}, genesis
	}

	coordinator := ibctesting.NewCustomAppCoordinator(t, 2, creator)
	require.Len(t, coordinator.Chains, 2)
	chainA := coordinator.GetChain(ibctesting.GetChainID(1))
	chainB := coordinator.GetChain(ibctesting.GetChainID(2))
	sourceApp := guruAppFromChain(t, chainA)
	hubApp := guruAppFromChain(t, chainB)
	t.Cleanup(func() {
		require.NoError(t, sourceApp.Close())
		require.NoError(t, hubApp.Close())
	})
	// Keep Guru's positive minimum gas price in genesis so the production
	// invariant is exercised. After initialization, disable the global-fee ante
	// check only in this in-process harness: ibc-go's generic coordinator signs
	// its client/connection/channel setup transactions with a fixed zero fee.
	for _, chainApp := range []struct {
		chain *ibctesting.TestChain
		app   *guruapp.App
	}{{chainA, sourceApp}, {chainB, hubApp}} {
		params := chainApp.app.FeeMarketKeeper.GetParams(chainApp.chain.GetContext())
		params.MinGasPrice = sdkmath.LegacyZeroDec()
		require.NoError(t, chainApp.app.FeeMarketKeeper.SetParams(chainApp.chain.GetContext(), params))
	}
	coordinator.CommitBlock(chainA, chainB)

	path := ibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig.PortID = transwaptypes.PortID
	path.EndpointB.ChannelConfig.PortID = transwaptypes.PortID
	path.EndpointA.ChannelConfig.Version = transwaptypes.V1
	path.EndpointB.ChannelConfig.Version = transwaptypes.V1
	path.EndpointA.ChannelConfig.Order = channeltypes.UNORDERED
	path.EndpointB.ChannelConfig.Order = channeltypes.UNORDERED
	path.Setup()
	require.NotEmpty(t, path.EndpointA.ChannelID)
	require.NotEmpty(t, path.EndpointB.ChannelID)

	hubCtx := chainB.GetContext()
	moderator, err := hubApp.ConstitutionKeeper.GetModeratorAddress(hubCtx)
	require.NoError(t, err)
	admin := mustAppAddress(t, hubApp, 0x52)
	require.NoError(t, hubApp.BexKeeper.RegisterAdmin(hubCtx, moderator, admin))
	exchange, err := hubApp.BexKeeper.RegisterExchange(hubCtx, &bexv1.MsgRegisterExchange{
		BexAdminAddress:           admin,
		ExchangeAdminAddress:      admin,
		DenomA:                    inputDenom,
		PortA:                     transwaptypes.PortID,
		ChannelA:                  path.EndpointB.ChannelID,
		DenomB:                    outputDenom,
		PortB:                     transwaptypes.PortID,
		ChannelB:                  path.EndpointB.ChannelID,
		OracleSymbolAToB:          "FOO/BAR",
		OracleSymbolBToA:          "BAR/FOO",
		FeeBpsAToB:                100,
		FeeBpsBToA:                100,
		LimitAToB:                 "1000000",
		LimitBToA:                 "1000000",
		VolumeCapAToB:             "1000000",
		VolumeCapBToA:             "1000000",
		Status:                    bexv1.ExchangeStatus_EXCHANGE_STATUS_ACTIVE,
		Metadata:                  map[string]string{"test": "proof-relayed-refund-recovery"},
		VolumeEpochSeconds:        86_400,
		MaxOracleStalenessSeconds: 3_600,
	})
	require.NoError(t, err)
	require.NoError(t, hubApp.OracleKeeper.SetLatestValue(hubCtx, &oracletypes.OracleValue{
		Symbol:        "FOO/BAR",
		ValueType:     oracletypes.ValueType_VALUE_TYPE_NUMERIC,
		Value:         "1",
		BlockHeight:   hubCtx.BlockHeight(),
		BlockTimeUnix: hubCtx.BlockTime().Unix(),
	}))
	coordinator.CommitBlock(chainB)

	// Seed the Hub's output-side BEX reserve through a real TransSwap packet.
	// This also creates the source-chain native escrow that a successful output
	// would eventually unescrow.
	liquidityTimeout := uint64(coordinator.CurrentTime.Add(time.Hour).UnixNano()) //nolint:gosec // fixed positive test time.
	liquidityPacket := sendNativeTranswapPacket(
		t,
		path.EndpointA,
		sourceApp,
		outputDenom,
		outputLiquidity,
		"0",
		chainB.SenderAccount.GetAddress().String(),
		"",
		liquidityTimeout,
	)
	require.NoError(t, path.RelayPacket(liquidityPacket))
	hubCtx = chainB.GetContext()
	hubLiquidityHolder := chainB.SenderAccount.GetAddress()
	liquidity := sdk.NewInt64Coin(exchange.GetIbcDenomB(), outputLiquidity)
	require.Equal(t, liquidity.Amount, hubApp.BankKeeper.GetBalance(hubCtx, hubLiquidityHolder, liquidity.Denom).Amount)
	require.NoError(t, hubApp.BexKeeper.ReceiveToReserve(
		hubCtx,
		exchange.GetId(),
		hubLiquidityHolder,
		sdk.NewCoins(liquidity),
	))
	coordinator.CommitBlock(chainB)
	require.NoError(t, hubApp.BexKeeper.AssertInvariants(chainB.GetContext()))

	// Send the actual exchange request through IBC. Its timestamp is the
	// immutable business deadline and becomes the original output timeout.
	originalTimeout := uint64(coordinator.CurrentTime.Add(refundTestWindow).UnixNano()) //nolint:gosec // fixed positive test time.
	inputPacket := sendNativeTranswapPacket(
		t,
		path.EndpointA,
		sourceApp,
		inputDenom,
		inputAmount,
		strconv.FormatUint(exchange.GetId(), 10),
		chainA.SenderAccount.GetAddress().String(),
		`guru.transwap.protection:v1:{"min_amount_out":"101","expected_exchange_revision":"1"}`,
		originalTimeout,
	)
	require.Zero(t, sourceApp.BankKeeper.GetBalance(
		chainA.GetContext(),
		chainA.SenderAccount.GetAddress(),
		inputDenom,
	).Amount.Sign())
	require.NoError(t, path.RelayPacket(inputPacket))
	require.Empty(t, sourceApp.IBCKeeper.ChannelKeeper.GetPacketCommitment(
		chainA.GetContext(),
		inputPacket.SourcePort,
		inputPacket.SourceChannel,
		inputPacket.Sequence,
	))

	hubCtx = chainB.GetContext()
	records := hubApp.TranswapKeeper.GetAllRefundRecords(hubCtx)
	require.Len(t, records, 1)
	pending := records[0]
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_PENDING, pending.GetStatus())
	require.Equal(t, originalTimeout, pending.GetOriginalTimeoutTimestamp())
	require.Zero(t, pending.GetRetryCount())
	outputPacket := originalOutputPacket(t, hubApp, hubCtx, pending, outputDenom, outputAmount)
	require.Equal(t, pending.GetOriginalOutputPacketCommitment(), channeltypes.CommitPacket(outputPacket))
	require.Equal(t, channeltypes.CommitPacket(outputPacket), packetCommitment(hubApp, hubCtx, outputPacket))
	requireRefundAccounting(t, hubApp, hubCtx, exchange.GetId(), exchange.GetIbcDenomA(), inputAmount, inputAmount-outputAmount)
	requireVolumeUsed(t, hubApp, hubCtx, exchange, outputAmount)
	require.NoError(t, hubApp.TranswapKeeper.AssertRefundInvariants(hubCtx))

	// Prove non-receipt on chain A after the original business timeout. IBC
	// core verifies the proof and removes the output commitment before the
	// TransSwap callback dispatches attempt 1 with a fresh transport timeout.
	advancePastPacketTimeout(t, coordinator, path.EndpointB, outputPacket)
	hubCtx = chainB.GetContext()
	firstAttempt := mustSingleRefund(t, hubApp, hubCtx)
	requireVolumeUsed(t, hubApp, hubCtx, exchange, 0)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_IN_FLIGHT, firstAttempt.GetStatus())
	require.Equal(t, uint32(1), firstAttempt.GetRetryCount())
	require.Equal(t, originalTimeout, firstAttempt.GetOriginalTimeoutTimestamp())
	require.Greater(t, firstAttempt.GetActiveTimeoutTimestamp(), originalTimeout)
	requireFreshRefundTimeout(t, hubApp, chainB, hubCtx, firstAttempt)
	require.Empty(t, packetCommitment(hubApp, hubCtx, outputPacket))
	firstRefundPacket := activeRefundPacket(t, hubApp, hubCtx, firstAttempt)
	require.Equal(t, channeltypes.CommitPacket(firstRefundPacket), packetCommitment(hubApp, hubCtx, firstRefundPacket))
	requireRefundAccounting(t, hubApp, hubCtx, exchange.GetId(), exchange.GetIbcDenomA(), inputAmount, 0)
	require.Equal(t, sdk.NewInt64Coin(exchange.GetIbcDenomB(), outputLiquidity).Amount,
		hubApp.BankKeeper.GetBalance(hubCtx, hubApp.BexKeeper.GetReserveAddress(hubCtx, exchange.GetId()), exchange.GetIbcDenomB()).Amount)
	require.NoError(t, hubApp.TranswapKeeper.AssertRefundInvariants(hubCtx))

	// Prove non-receipt of attempt 1. The retry must use a new sequence and a
	// timeout recalculated from the now-later destination client timestamp.
	advancePastPacketTimeout(t, coordinator, path.EndpointB, firstRefundPacket)
	hubCtx = chainB.GetContext()
	secondAttempt := mustSingleRefund(t, hubApp, hubCtx)
	require.Equal(t, transwaptypes.RefundStatus_REFUND_STATUS_IN_FLIGHT, secondAttempt.GetStatus())
	require.Equal(t, uint32(2), secondAttempt.GetRetryCount())
	require.NotEqual(t, firstAttempt.GetActivePacketSequence(), secondAttempt.GetActivePacketSequence())
	require.Greater(t, secondAttempt.GetActiveTimeoutTimestamp(), firstAttempt.GetActiveTimeoutTimestamp())
	require.Equal(t, originalTimeout, secondAttempt.GetOriginalTimeoutTimestamp())
	requireFreshRefundTimeout(t, hubApp, chainB, hubCtx, secondAttempt)
	require.Empty(t, packetCommitment(hubApp, hubCtx, firstRefundPacket))
	secondRefundPacket := activeRefundPacket(t, hubApp, hubCtx, secondAttempt)
	require.Equal(t, channeltypes.CommitPacket(secondRefundPacket), packetCommitment(hubApp, hubCtx, secondRefundPacket))
	require.NoError(t, hubApp.TranswapKeeper.AssertRefundInvariants(hubCtx))

	// Relay attempt 2 and its acknowledgement with real membership proofs on
	// both chains. The source receives exactly one native refund and the Hub
	// releases the aggregate BEX liability only after ACK success.
	require.NoError(t, path.RelayPacket(secondRefundPacket))
	hubCtx = chainB.GetContext()
	require.Empty(t, hubApp.TranswapKeeper.GetAllRefundRecords(hubCtx), "completed refund record must be pruned")
	requireRefundAccounting(t, hubApp, hubCtx, exchange.GetId(), exchange.GetIbcDenomA(), 0, 0)
	requireVolumeUsed(t, hubApp, hubCtx, exchange, 0)
	require.Equal(t, sdk.NewInt64Coin(inputDenom, inputAmount).Amount,
		sourceApp.BankKeeper.GetBalance(chainA.GetContext(), chainA.SenderAccount.GetAddress(), inputDenom).Amount)
	require.Empty(t, packetCommitment(hubApp, hubCtx, secondRefundPacket))
	require.NoError(t, hubApp.TranswapKeeper.AssertRefundInvariants(hubCtx))
	require.NoError(t, hubApp.BexKeeper.AssertInvariants(hubCtx))
}

func guruAppFromChain(t *testing.T, chain *ibctesting.TestChain) *guruapp.App {
	t.Helper()
	wrapped, ok := chain.App.(testingGuruApp)
	require.True(t, ok)
	return wrapped.App
}

func sendNativeTranswapPacket(
	t *testing.T,
	endpoint *ibctesting.Endpoint,
	app *guruapp.App,
	denom string,
	amount int64,
	exchangeID string,
	receiver string,
	memo string,
	timeoutTimestamp uint64,
) channeltypes.Packet {
	t.Helper()
	ctx := endpoint.Chain.GetContext()
	sender := endpoint.Chain.SenderAccount.GetAddress()
	coin := sdk.NewInt64Coin(denom, amount)
	require.NoError(t, app.BankKeeper.MintCoins(ctx, transwaptypes.ModuleName, sdk.NewCoins(coin)))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, transwaptypes.ModuleName, sender, sdk.NewCoins(coin)))
	token := &transwaptypes.Token{Denom: transwaptypes.NewDenom(denom), Amount: coin.Amount.String()}
	require.NoError(t, app.TranswapKeeper.SendTransfer(
		ctx,
		transwaptypes.PortID,
		endpoint.ChannelID,
		token,
		sender,
	))
	data := &transwaptypes.FungibleTokenPacketData{
		ExchangeId: exchangeID,
		Denom:      denom,
		Amount:     coin.Amount.String(),
		Sender:     sender.String(),
		Receiver:   receiver,
		Memo:       memo,
	}
	packetData := transwaptypes.FungibleTokenPacketDataBytes(data)
	sequence, err := endpoint.SendPacket(clienttypes.ZeroHeight(), timeoutTimestamp, packetData)
	require.NoError(t, err)
	return channeltypes.NewPacket(
		packetData,
		sequence,
		endpoint.ChannelConfig.PortID,
		endpoint.ChannelID,
		endpoint.Counterparty.ChannelConfig.PortID,
		endpoint.Counterparty.ChannelID,
		clienttypes.ZeroHeight(),
		timeoutTimestamp,
	)
}

func originalOutputPacket(
	t *testing.T,
	app *guruapp.App,
	ctx sdk.Context,
	record *transwaptypes.RefundRecord,
	denom string,
	amount int64,
) channeltypes.Packet {
	t.Helper()
	channel, found := app.IBCKeeper.ChannelKeeper.GetChannel(ctx, record.GetOriginalOutputPort(), record.GetOriginalOutputChannel())
	require.True(t, found)
	exchangeID, err := strconv.ParseUint(record.GetExchangeId(), 10, 64)
	require.NoError(t, err)
	data := transwaptypes.NewFungibleTokenPacketData(
		transwaptypes.DenomPath(transwaptypes.NewDenom(denom, transwaptypes.NewHop(record.GetOriginalOutputPort(), record.GetOriginalOutputChannel()))),
		strconv.FormatInt(amount, 10),
		app.BexKeeper.GetReserveAddress(ctx, exchangeID).String(),
		record.GetReceiver(),
		"Station exchange",
	)
	return channeltypes.NewPacket(
		transwaptypes.FungibleTokenPacketDataBytes(data),
		record.GetOriginalOutputSequence(),
		record.GetOriginalOutputPort(),
		record.GetOriginalOutputChannel(),
		channel.Counterparty.PortId,
		channel.Counterparty.ChannelId,
		clienttypes.ZeroHeight(),
		record.GetOriginalTimeoutTimestamp(),
	)
}

func activeRefundPacket(t *testing.T, app *guruapp.App, ctx sdk.Context, record *transwaptypes.RefundRecord) channeltypes.Packet {
	t.Helper()
	channel, found := app.IBCKeeper.ChannelKeeper.GetChannel(ctx, record.GetRefundSourcePort(), record.GetRefundSourceChannel())
	require.True(t, found)
	exchangeID, err := strconv.ParseUint(record.GetExchangeId(), 10, 64)
	require.NoError(t, err)
	token := record.GetToken()
	data := transwaptypes.NewFungibleTokenPacketData(
		transwaptypes.DenomPath(token.GetDenom()),
		token.GetAmount(),
		app.BexKeeper.GetReserveAddress(ctx, exchangeID).String(),
		record.GetReceiver(),
		record.GetMemo(),
	)
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

func advancePastPacketTimeout(
	t *testing.T,
	coordinator *ibctesting.Coordinator,
	sourceEndpoint *ibctesting.Endpoint,
	packet channeltypes.Packet,
) {
	t.Helper()
	require.LessOrEqual(t, packet.TimeoutTimestamp, uint64(^uint64(0)>>1))
	coordinator.SetTime(time.Unix(0, int64(packet.TimeoutTimestamp)).Add(time.Second)) //nolint:gosec // bounded above.
	require.NoError(t, sourceEndpoint.UpdateClient())
	require.NoError(t, sourceEndpoint.TimeoutPacket(packet))
}

func mustSingleRefund(t *testing.T, app *guruapp.App, ctx sdk.Context) *transwaptypes.RefundRecord {
	t.Helper()
	records := app.TranswapKeeper.GetAllRefundRecords(ctx)
	require.Len(t, records, 1)
	return records[0]
}

func packetCommitment(app *guruapp.App, ctx sdk.Context, packet channeltypes.Packet) []byte {
	return app.IBCKeeper.ChannelKeeper.GetPacketCommitment(
		ctx,
		packet.SourcePort,
		packet.SourceChannel,
		packet.Sequence,
	)
}

func requireRefundAccounting(
	t *testing.T,
	app *guruapp.App,
	ctx sdk.Context,
	exchangeID uint64,
	denom string,
	pendingAmount int64,
	lockedAmount int64,
) {
	t.Helper()
	pending, err := app.BexKeeper.GetPendingLiabilities(ctx, exchangeID)
	require.NoError(t, err)
	locked, err := app.BexKeeper.GetLockedFees(ctx, exchangeID)
	require.NoError(t, err)
	expectedPending := sdk.NewCoins()
	if pendingAmount > 0 {
		expectedPending = sdk.NewCoins(sdk.NewInt64Coin(denom, pendingAmount))
	}
	expectedLocked := sdk.NewCoins()
	if lockedAmount > 0 {
		expectedLocked = sdk.NewCoins(sdk.NewInt64Coin(denom, lockedAmount))
	}
	require.Equal(t, expectedPending, pending)
	require.Equal(t, expectedLocked, locked)
}

func requireVolumeUsed(
	t *testing.T,
	app *guruapp.App,
	ctx sdk.Context,
	exchange *bexv1.Exchange,
	want int64,
) {
	t.Helper()
	used, err := app.BexKeeper.GetCurrentVolumeAmount(
		ctx,
		exchange,
		bexv1.SwapDirection_SWAP_DIRECTION_A_TO_B,
	)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin("uvolume", want).Amount, used)
}

func requireFreshRefundTimeout(
	t *testing.T,
	app *guruapp.App,
	chain *ibctesting.TestChain,
	ctx sdk.Context,
	record *transwaptypes.RefundRecord,
) {
	t.Helper()
	channel, found := app.IBCKeeper.ChannelKeeper.GetChannel(ctx, record.GetRefundSourcePort(), record.GetRefundSourceChannel())
	require.True(t, found)
	require.Len(t, channel.ConnectionHops, 1)
	connection, found := app.IBCKeeper.ConnectionKeeper.GetConnection(ctx, channel.ConnectionHops[0])
	require.True(t, found)
	consensus, found := app.IBCKeeper.ClientKeeper.GetLatestClientConsensusState(ctx, connection.ClientId)
	require.True(t, found)
	params, err := app.TranswapKeeper.GetParams(ctx)
	require.NoError(t, err)
	blockTimestamp := uint64(chain.LatestCommittedHeader.GetTime().UnixNano()) //nolint:gosec // committed test time is positive.
	destinationTimestamp := consensus.GetTimestamp()                           //nolint:staticcheck // mirrors the IBC-Go v11 application-keeper API used by production.
	expected := max(blockTimestamp, destinationTimestamp) + params.GetRefundTimeoutWindow()
	require.Equal(t, expected, record.GetActiveTimeoutTimestamp())
	require.Greater(t, record.GetActiveTimeoutTimestamp(), destinationTimestamp+params.GetMinRelaySafetyMargin())
}

func mustAppAddress(t *testing.T, app *guruapp.App, fill byte) string {
	t.Helper()
	address, err := app.AccountKeeper.AddressCodec().BytesToString(bytes.Repeat([]byte{fill}, 20))
	require.NoError(t, err)
	return address
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
