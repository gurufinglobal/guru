package app

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	antetypes "github.com/cosmos/evm/ante/types"

	appante "github.com/gurufinglobal/guru/v3/app/ante"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

const standardMsgSendGasRuntimeWorkerEnv = "GURU_STANDARD_MSGSEND_RUNTIME_WORKER"

func TestStandardMsgSendGasRuntimeContract(t *testing.T) {
	if !runFeePolicyRuntimeWorker(t, standardMsgSendGasRuntimeWorkerEnv) {
		return
	}

	fixture := newFeePolicyRuntimeFixture(t, feePolicyRuntimeGenesisOptions{
		noBaseFee:   true,
		baseFee:     sdkmath.LegacyZeroDec(),
		minGasPrice: sdkmath.LegacyOneDec(),
	})
	recipient := fixture.actor(feePolicyActorRecipient)

	t.Run("simulation check and finalize expose 21k with account FeePolicy applied", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorNormal)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)
		beforeCollector := fixture.collectorBalance()
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		txBytes := fixture.signTxWithFee(
			t,
			payer,
			[]sdk.Msg{msg},
			feePolicyRuntimeCoins(int64(appante.StandardMsgSendGas)),
			feePolicyRuntimeTxOptions{
				gasLimit: appante.StandardMsgSendGas,
			},
		)

		gasInfo, _, err := fixture.app.Simulate(txBytes)
		require.NoError(t, err)
		require.Equal(t, appante.StandardMsgSendGas, gasInfo.GasWanted)
		require.Equal(t, appante.StandardMsgSendGas, gasInfo.GasUsed)

		checkResult, err := fixture.app.CheckTx(&abci.RequestCheckTx{
			Tx:   txBytes,
			Type: abci.CheckTxType_New,
		})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, checkResult.Code, checkResult.Log)
		require.Equal(t, int64(appante.StandardMsgSendGas), checkResult.GasWanted)
		require.Equal(t, int64(appante.StandardMsgSendGas), checkResult.GasUsed)

		result := fixture.finalize(t, txBytes)
		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasWanted)
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasUsed)
		requireIntEqual(t, beforePayer.SubRaw(15_751), fixture.balance(payer.address))
		requireIntEqual(t, beforeRecipient.AddRaw(1), fixture.balance(recipient.address))
		requireIntEqual(t, beforeCollector.AddRaw(15_750), fixture.collectorBalance())
		require.Equal(t, uint64(1), fixture.sequence(payer.address))
		fixture.requireFeeEvent(t, result, 15_750, payer.addressString)
	})

	t.Run("dynamic fee extension remains in the bounded class", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorDynamic)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)
		msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
		txBytes := fixture.signTxWithFee(
			t,
			payer,
			[]sdk.Msg{msg},
			feePolicyRuntimeCoins(int64(appante.StandardMsgSendGas)),
			feePolicyRuntimeTxOptions{
				gasLimit: appante.StandardMsgSendGas,
				extension: &antetypes.ExtensionOptionDynamicFeeTx{
					MaxPriorityPrice: sdkmath.LegacyOneDec(),
				},
			},
		)

		result := fixture.finalize(t, txBytes)
		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasWanted)
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasUsed)
		requireIntEqual(t, beforePayer.SubRaw(12_346), fixture.balance(payer.address))
		requireIntEqual(t, beforeRecipient.AddRaw(1), fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.NewInt(12_345), fixture.collectorBalance())
		fixture.requireFeeEvent(t, result, 12_345, payer.addressString)
	})

	t.Run("new recipient account remains eligible", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorBoundary)
		newRecipient := sdk.AccAddress([]byte("standard-new-recipient"))
		beforePayer := fixture.balance(payer.address)
		require.Nil(t, fixture.app.AccountKeeper.GetAccount(fixture.committedContext(), newRecipient))
		msg := banktypes.NewMsgSend(payer.address, newRecipient, feePolicyRuntimeCoins(1))
		result := fixture.finalize(t, fixture.signTxWithFee(
			t,
			payer,
			[]sdk.Msg{msg},
			feePolicyRuntimeCoins(int64(appante.StandardMsgSendGas)),
			feePolicyRuntimeTxOptions{
				gasLimit: appante.StandardMsgSendGas,
			},
		))

		require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasUsed)
		requireIntEqual(t, beforePayer.SubRaw(15_751), fixture.balance(payer.address))
		requireIntEqual(t, sdkmath.OneInt(), fixture.app.BankKeeper.GetBalance(
			fixture.committedContext(),
			newRecipient,
			appparams.BaseDenom,
		).Amount)
		requireIntEqual(t, sdkmath.NewInt(15_750), fixture.collectorBalance())
		require.NotNil(t, fixture.app.AccountKeeper.GetAccount(fixture.committedContext(), newRecipient))
	})

	t.Run("message failure commits fee and sequence but rolls back send", func(t *testing.T) {
		payer := fixture.actor(feePolicyActorZero)
		beforePayer := fixture.balance(payer.address)
		beforeRecipient := fixture.balance(recipient.address)
		msg := banktypes.NewMsgSend(
			payer.address,
			recipient.address,
			feePolicyRuntimeCoins(feePolicyRuntimeInitialFunds+1),
		)
		result := fixture.finalize(t, fixture.signTxWithFee(
			t,
			payer,
			[]sdk.Msg{msg},
			feePolicyRuntimeCoins(int64(appante.StandardMsgSendGas)),
			feePolicyRuntimeTxOptions{
				gasLimit: appante.StandardMsgSendGas,
			},
		))

		require.NotEqual(t, abci.CodeTypeOK, result.Code)
		require.Contains(t, result.Log, "insufficient funds")
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasWanted)
		require.Equal(t, int64(appante.StandardMsgSendGas), result.GasUsed)
		requireIntEqual(t, beforePayer, fixture.balance(payer.address))
		requireIntEqual(t, beforeRecipient, fixture.balance(recipient.address))
		requireIntEqual(t, sdkmath.ZeroInt(), fixture.collectorBalance())
		require.Equal(t, uint64(1), fixture.sequence(payer.address))
	})
}

func TestStandardMsgSendGasIgnoresInternalSafetyLimit(t *testing.T) {
	if !runFeePolicyRuntimeWorker(t, standardMsgSendGasRuntimeWorkerEnv+"_OOG") {
		return
	}

	fixture := newFeePolicyRuntimeFixture(t, feePolicyRuntimeGenesisOptions{
		noBaseFee:   true,
		baseFee:     sdkmath.LegacyZeroDec(),
		minGasPrice: sdkmath.LegacyOneDec(),
	})
	payer := fixture.actor(feePolicyActorGrantSigPayer)
	recipient := fixture.actor(feePolicyActorRecipient)
	beforePayer := fixture.balance(payer.address)
	beforeRecipient := fixture.balance(recipient.address)
	beforeCollector := fixture.collectorBalance()
	msg := banktypes.NewMsgSend(payer.address, recipient.address, feePolicyRuntimeCoins(1))
	result := fixture.finalize(t, fixture.signTxWithFee(
		t,
		payer,
		[]sdk.Msg{msg},
		feePolicyRuntimeCoins(int64(appante.StandardMsgSendGas)),
		feePolicyRuntimeTxOptions{
			gasLimit: appante.StandardMsgSendGas,
		},
	))

	require.Equal(t, abci.CodeTypeOK, result.Code, result.Log)
	require.Equal(t, int64(appante.StandardMsgSendGas), result.GasWanted)
	require.Equal(t, int64(appante.StandardMsgSendGas), result.GasUsed)
	requireIntEqual(t, beforePayer.SubRaw(12_346), fixture.balance(payer.address))
	requireIntEqual(t, beforeRecipient.AddRaw(1), fixture.balance(recipient.address))
	requireIntEqual(t, beforeCollector.AddRaw(12_345), fixture.collectorBalance())
	require.Equal(t, uint64(1), fixture.sequence(payer.address))
	fixture.requireFeeEvent(t, result, 12_345, payer.addressString)
}
