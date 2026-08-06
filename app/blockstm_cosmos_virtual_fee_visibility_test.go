package app

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	"github.com/stretchr/testify/require"
)

const blockSTMCosmosVirtualFeeVisibilityWorkerEnv = "GURU_BLOCKSTM_COSMOS_VIRTUAL_FEE_VISIBILITY_WORKER"

// TestBlockSTMCosmosVirtualFeeVisibility documents the consensus-visible semantic
// boundary introduced by virtual fee collection:
//
//   - ante deducts the payer's real balance immediately, but credits the fee
//     collector's real balance only from x/bank EndBlock;
//   - coin_spent/transfer remain transaction events, while the matching
//     coin_received is an aggregated block event;
//   - a later ante failure discards both the real debit and virtual credit; and
//   - the per-block virtual accumulator cannot credit the next block again.
func TestBlockSTMCosmosVirtualFeeVisibility(t *testing.T) {
	if !runFeePolicyRuntimeWorker(t, blockSTMCosmosVirtualFeeVisibilityWorkerEnv) {
		return
	}

	fixture := newFeePolicyRuntimeFixture(t, feePolicyRuntimeGenesisOptions{
		noBaseFee:   true,
		baseFee:     sdkmath.LegacyZeroDec(),
		minGasPrice: sdkmath.LegacyOneDec(),
	})
	collector := fixture.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	collectorString := collector.String()
	expectedFee := feePolicyRuntimeCoins(150_000)
	recipient := fixture.actor(feePolicyActorRecipient)

	t.Run("late ante failure rolls back real and virtual fee state", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorGrantSigPayer)
		granter := fixture.actor(feePolicyActorGrantSigGranter)
		beforePayer := fixture.balance(payer.address)
		beforeGranter := fixture.balance(granter.address)
		beforeCollector := fixture.collectorBalance()
		beforeAllowance := fixture.allowance(t, granter, payer)

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		response := finalizeBlockSTMCosmosVirtualFeeVisibility(t, fixture, fixture.signTx(
			t,
			payer,
			[]sdk.Msg{msg},
			feePolicyRuntimeTxOptions{feeGranter: granter.address, corruptSig: true},
		))

		require.Len(t, response.TxResults, 1)
		require.NotEqual(t, abci.CodeTypeOK, response.TxResults[0].Code)
		requireIntEqual(t, beforePayer, fixture.balance(payer.address))
		requireIntEqual(t, beforeGranter, fixture.balance(granter.address))
		requireIntEqual(t, beforeCollector, fixture.collectorBalance())
		requireIntEqual(t, beforeAllowance, fixture.allowance(t, granter, payer))
		require.Equal(t, uint64(0), fixture.sequence(payer.address))
		require.False(t, blockSTMCosmosVirtualFeeHasEvent(
			response.Events,
			banktypes.EventTypeCoinReceived,
			map[string]string{
				banktypes.AttributeKeyReceiver: collectorString,
				sdk.AttributeKeyAmount:         expectedFee.String(),
			},
		))
	})

	t.Run("fee receiver becomes real only in end block and only once", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorNormal)
		beforePayer := fixture.balance(payer.address)
		beforeCollector := fixture.collectorBalance()

		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(7))
		response := finalizeBlockSTMCosmosVirtualFeeVisibility(
			t,
			fixture,
			fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{}),
		)

		require.Len(t, response.TxResults, 1)
		require.Equal(t, abci.CodeTypeOK, response.TxResults[0].Code, response.TxResults[0].Log)
		requireIntEqual(t, beforePayer.SubRaw(150_007), fixture.balance(payer.address))
		requireIntEqual(t, beforeCollector.AddRaw(150_000), fixture.collectorBalance())

		// The fee collector has not received real coins while this transaction's
		// result is assembled. MsgSend's unrelated recipient coin_received event
		// may still be present, so match the receiver and amount exactly.
		require.False(t, blockSTMCosmosVirtualFeeHasEvent(
			response.TxResults[0].Events,
			banktypes.EventTypeCoinReceived,
			map[string]string{
				banktypes.AttributeKeyReceiver: collectorString,
				sdk.AttributeKeyAmount:         expectedFee.String(),
			},
		))
		require.True(t, blockSTMCosmosVirtualFeeHasEvent(
			response.Events,
			banktypes.EventTypeCoinReceived,
			map[string]string{
				banktypes.AttributeKeyReceiver: collectorString,
				sdk.AttributeKeyAmount:         expectedFee.String(),
			},
		), "fee collector credit must be emitted by x/bank EndBlock")

		afterFirstCredit := fixture.collectorBalance()
		requireIntEqual(t, sdkmath.NewInt(150_000), afterFirstCredit)
		emptyResponse := finalizeBlockSTMCosmosVirtualFeeVisibility(t, fixture)
		require.Empty(t, emptyResponse.TxResults)
		// x/constitution consumes the preceding block's collector balance in
		// BeginBlock. A stale virtual object entry would then credit 150_000 back
		// in bank EndBlock, so an exact zero proves that entry did not survive the
		// block boundary.
		requireIntEqual(t, sdkmath.ZeroInt(), fixture.collectorBalance())
		require.False(t, blockSTMCosmosVirtualFeeHasEvent(
			emptyResponse.Events,
			banktypes.EventTypeCoinReceived,
			map[string]string{
				banktypes.AttributeKeyReceiver: collectorString,
				sdk.AttributeKeyAmount:         expectedFee.String(),
			},
		), "the previous block's virtual accumulator must not be replayed")
	})

	t.Run("ante exposes the real debit before the end block credit", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorSelfGranter)
		beforePayer := fixture.balance(payer.address)
		beforeCollector := fixture.collectorBalance()
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		txBytes := fixture.signTx(t, payer, []sdk.Msg{msg}, feePolicyRuntimeTxOptions{})
		tx, err := fixture.app.TxConfig().TxDecoder()(txBytes)
		require.NoError(t, err)

		ctx := fixture.app.NewNextBlockContext(cmtproto.Header{
			ChainID: feePolicyRuntimeChainID,
			Height:  fixture.nextBlock,
			Time:    fixture.startTime.Add(time.Duration(fixture.nextBlock) * time.Second),
		}).WithTxIndex(0)
		anteCtx, _ := ctx.CacheContext()
		anteCtx, err = fixture.app.AnteHandler()(anteCtx, tx, false)
		require.NoError(t, err)

		requireIntEqual(t, beforePayer.SubRaw(150_000), fixture.app.BankKeeper.GetBalance(
			anteCtx,
			payer.address,
			appparams.BaseDenom,
		).Amount)
		requireIntEqual(t, beforeCollector, fixture.app.BankKeeper.GetBalance(
			anteCtx,
			collector,
			appparams.BaseDenom,
		).Amount)
		anteEvents := anteCtx.EventManager().ABCIEvents()
		require.True(t, blockSTMCosmosVirtualFeeHasEvent(
			anteEvents,
			banktypes.EventTypeCoinSpent,
			map[string]string{
				banktypes.AttributeKeySpender: payer.address.String(),
				sdk.AttributeKeyAmount:        expectedFee.String(),
			},
		))
		require.True(t, blockSTMCosmosVirtualFeeHasEvent(
			anteEvents,
			banktypes.EventTypeTransfer,
			map[string]string{
				banktypes.AttributeKeyRecipient: collectorString,
				banktypes.AttributeKeySender:    payer.address.String(),
				sdk.AttributeKeyAmount:          expectedFee.String(),
			},
		))
		require.False(t, blockSTMCosmosVirtualFeeHasEvent(
			anteEvents,
			banktypes.EventTypeCoinReceived,
			map[string]string{banktypes.AttributeKeyReceiver: collectorString},
		))

		endBlockCtx := anteCtx.WithEventManager(sdk.NewEventManager())
		require.NoError(t, fixture.app.BankKeeper.CreditVirtualAccounts(endBlockCtx))
		requireIntEqual(t, beforeCollector.AddRaw(150_000), fixture.app.BankKeeper.GetBalance(
			endBlockCtx,
			collector,
			appparams.BaseDenom,
		).Amount)
		require.True(t, blockSTMCosmosVirtualFeeHasEvent(
			endBlockCtx.EventManager().ABCIEvents(),
			banktypes.EventTypeCoinReceived,
			map[string]string{
				banktypes.AttributeKeyReceiver: collectorString,
				sdk.AttributeKeyAmount:         expectedFee.String(),
			},
		))
	})
}

func finalizeBlockSTMCosmosVirtualFeeVisibility(
	t *testing.T,
	fixture *feePolicyRuntimeFixture,
	txs ...[]byte,
) *abci.ResponseFinalizeBlock {
	t.Helper()

	response, err := fixture.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: fixture.nextBlock,
		Time:   fixture.startTime.Add(time.Duration(fixture.nextBlock) * time.Second),
		Txs:    txs,
	})
	require.NoError(t, err)
	_, err = fixture.app.Commit()
	require.NoError(t, err)
	fixture.nextBlock++
	return response
}

func blockSTMCosmosVirtualFeeHasEvent(
	events []abci.Event,
	eventType string,
	expectedAttributes map[string]string,
) bool {
	for _, event := range events {
		if event.Type != eventType {
			continue
		}

		attributes := make(map[string]string, len(event.Attributes))
		for _, attribute := range event.Attributes {
			attributes[attribute.Key] = attribute.Value
		}
		matches := true
		for key, expectedValue := range expectedAttributes {
			if attributes[key] != expectedValue {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}

	return false
}
