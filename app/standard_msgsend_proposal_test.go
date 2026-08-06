package app

import (
	"context"
	"math"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	appante "github.com/gurufinglobal/guru/v3/app/ante"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
)

func TestStandardMsgSendProposalGasUsesStandardizedGasForCandidates(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	candidate, candidateBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		appante.StandardMsgSendGas,
		sdk.NewCoins(sdk.NewInt64Coin("acandidate", 1)),
	)
	ordinary, ordinaryBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		500_000,
		sdk.NewCoins(
			sdk.NewInt64Coin("aordinary", 1),
			sdk.NewInt64Coin("bordinary", 1),
		),
	)

	require.True(t, appante.IsStandardMsgSendGasCandidate(candidate, candidateBytes))
	require.Equal(t, uint64(appante.StandardMsgSendGas), standardMsgSendProposalGas(candidate, candidateBytes))
	require.False(t, appante.IsStandardMsgSendGasCandidate(ordinary, ordinaryBytes))
	require.Equal(t, uint64(500_000), standardMsgSendProposalGas(ordinary, ordinaryBytes))
}

func TestStandardMsgSendTxSelectorGasBoundaries(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	candidate, candidateBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		appante.StandardMsgSendGas,
		sdk.NewCoins(sdk.NewInt64Coin("acandidate", 1)),
	)
	ordinary, ordinaryBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		500_000,
		sdk.NewCoins(
			sdk.NewInt64Coin("aordinary", 1),
			sdk.NewInt64Coin("bordinary", 1),
		),
	)

	t.Run("599_999 admits at most 28 candidates", func(t *testing.T) {
		selector := &standardMsgSendTxSelector{}
		for i := 1; i <= 28; i++ {
			require.False(t, selector.SelectTxForProposal(
				context.Background(), math.MaxUint64, 599_999, candidate, candidateBytes,
			))
			require.Len(t, selector.SelectedTxs(context.Background()), i)
		}
		require.False(t, selector.SelectTxForProposal(
			context.Background(), math.MaxUint64, 599_999, candidate, candidateBytes,
		))
		require.Len(t, selector.SelectedTxs(context.Background()), 28)
		require.Equal(t, uint64(588_000), selector.totalTxGas)
	})

	t.Run("600_000 admits exactly 28 candidates", func(t *testing.T) {
		selector := &standardMsgSendTxSelector{}
		for i := 1; i <= 28; i++ {
			require.False(t, selector.SelectTxForProposal(
				context.Background(), math.MaxUint64, 600_000, candidate, candidateBytes,
			))
			require.Len(t, selector.SelectedTxs(context.Background()), i)
		}
		require.False(t, selector.SelectTxForProposal(
			context.Background(), math.MaxUint64, 600_000, candidate, candidateBytes,
		))
		require.Len(t, selector.SelectedTxs(context.Background()), 28)
		require.Equal(t, uint64(588_000), selector.totalTxGas)
	})

	t.Run("30M admits exactly 1428 candidates", func(t *testing.T) {
		selector := &standardMsgSendTxSelector{}
		for i := 1; i <= 1428; i++ {
			require.False(t, selector.SelectTxForProposal(
				context.Background(), math.MaxUint64, 30_000_000, candidate, candidateBytes,
			))
			require.Len(t, selector.SelectedTxs(context.Background()), i)
		}
		require.Len(t, selector.SelectedTxs(context.Background()), 1428)
		require.Equal(t, uint64(29_988_000), selector.totalTxGas)

		// A caller that probes after accepting all candidates cannot add a 1429th
		// candidate.
		require.False(t, selector.SelectTxForProposal(
			context.Background(), math.MaxUint64, 30_000_000, candidate, candidateBytes,
		))
		require.Len(t, selector.SelectedTxs(context.Background()), 1428)
	})

	t.Run("ordinary transactions retain declared gas", func(t *testing.T) {
		selector := &standardMsgSendTxSelector{}
		require.False(t, selector.SelectTxForProposal(
			context.Background(), math.MaxUint64, 499_999, ordinary, ordinaryBytes,
		))
		require.Empty(t, selector.SelectedTxs(context.Background()))

		require.True(t, selector.SelectTxForProposal(
			context.Background(), math.MaxUint64, 500_000, ordinary, ordinaryBytes,
		))
		require.Len(t, selector.SelectedTxs(context.Background()), 1)
		require.Equal(t, uint64(500_000), selector.totalTxGas)
	})

	t.Run("zero and negative-one gas limits are byte-gated only", func(t *testing.T) {
		signedUnlimited := int64(-1)
		for _, maxGas := range []uint64{0, uint64(signedUnlimited)} {
			selector := &standardMsgSendTxSelector{}
			var accepted int
			for i := 0; i < 2_000; i++ {
				full := selector.SelectTxForProposal(
					context.Background(), math.MaxUint64, maxGas, candidate, candidateBytes,
				)
				if full {
					break
				}
				accepted++
			}
			require.GreaterOrEqual(t, accepted, 2_000)
			require.Len(t, selector.SelectedTxs(context.Background()), 2_000)
			require.Equal(t, uint64(42_000_000), selector.totalTxGas)
			require.False(t, selector.SelectTxForProposal(
				context.Background(), math.MaxUint64, maxGas, candidate, candidateBytes,
			))
			require.Len(t, selector.SelectedTxs(context.Background()), 2_001)
			require.False(t, selector.SelectTxForProposal(
				context.Background(), math.MaxUint64, maxGas, ordinary, ordinaryBytes,
			))
			require.Len(t, selector.SelectedTxs(context.Background()), 2_002)
		}
	})
}

func TestStandardMsgSendTxSelectorAccountsForProtoBytesAndClear(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	candidate, candidateBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		appante.StandardMsgSendGas,
		sdk.NewCoins(sdk.NewInt64Coin("acandidate", 1)),
	)
	oneTxBytes := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{candidateBytes}))
	twoTxBytes := uint64(cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{candidateBytes, candidateBytes}))
	require.Equal(t, 2*oneTxBytes, twoTxBytes)

	selector := &standardMsgSendTxSelector{}
	require.False(t, selector.SelectTxForProposal(
		context.Background(), twoTxBytes-1, 0, candidate, candidateBytes,
	))
	require.False(t, selector.SelectTxForProposal(
		context.Background(), twoTxBytes-1, 0, candidate, candidateBytes,
	))
	require.Len(t, selector.SelectedTxs(context.Background()), 1)
	require.Equal(t, oneTxBytes, selector.totalTxBytes)

	selector.Clear()
	require.Zero(t, selector.totalTxBytes)
	require.Zero(t, selector.totalTxGas)
	require.Nil(t, selector.selectedTxs)
	require.Empty(t, selector.SelectedTxs(context.Background()))

	require.False(t, selector.SelectTxForProposal(
		context.Background(), twoTxBytes, 0, candidate, candidateBytes,
	))
	require.True(t, selector.SelectTxForProposal(
		context.Background(), twoTxBytes, 0, candidate, candidateBytes,
	))
	require.Len(t, selector.SelectedTxs(context.Background()), 2)
	require.Equal(t, twoTxBytes, selector.totalTxBytes)
}

func TestStandardMsgSendProcessProposalRejectsOverBudgetCustomProposal(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	_, candidateBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		appante.StandardMsgSendGas,
		sdk.NewCoins(sdk.NewInt64Coin("acandidate", 1)),
	)
	testApp := &App{txConfig: encoding.TxConfig}

	innerCalls := 0
	acceptingNoOp := func(_ sdk.Context, _ *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		innerCalls++
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
	}
	handler := testApp.standardMsgSendProcessProposal(acceptingNoOp)
	ctx := standardMsgSendProposalContext(30_000_000)

	accepted, err := handler(ctx, &abci.RequestProcessProposal{
		Txs: standardMsgSendProposalRepeat(candidateBytes, 1428),
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, accepted.Status)
	require.Equal(t, 1, innerCalls)

	rejected, err := handler(ctx, &abci.RequestProcessProposal{
		Txs: standardMsgSendProposalRepeat(candidateBytes, 1429),
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, rejected.Status)
	require.Equal(t, 1, innerCalls, "over-budget proposal must be rejected before a no-op inner handler")
}

func TestStandardMsgSendProcessProposalUsesOrdinaryDeclaredGas(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	_, candidateBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		appante.StandardMsgSendGas,
		sdk.NewCoins(sdk.NewInt64Coin("acandidate", 1)),
	)
	_, ordinaryBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		30_000_000,
		sdk.NewCoins(
			sdk.NewInt64Coin("aordinary", 1),
			sdk.NewInt64Coin("bordinary", 1),
		),
	)
	testApp := &App{txConfig: encoding.TxConfig}

	innerCalls := 0
	handler := testApp.standardMsgSendProcessProposal(func(
		_ sdk.Context,
		_ *abci.RequestProcessProposal,
	) (*abci.ResponseProcessProposal, error) {
		innerCalls++
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
	})
	txs := append(standardMsgSendProposalRepeat(candidateBytes, 1), ordinaryBytes)
	response, err := handler(standardMsgSendProposalContext(30_000_000), &abci.RequestProcessProposal{Txs: txs})

	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, response.Status)
	require.Zero(t, innerCalls)
}

func TestStandardMsgSendProcessProposalUnlimitedGasIsByteGatedOnly(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	_, candidateBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		appante.StandardMsgSendGas,
		sdk.NewCoins(sdk.NewInt64Coin("acandidate", 1)),
	)
	_, ordinaryBytes := standardMsgSendProposalTestTx(
		t,
		encoding,
		500_000,
		sdk.NewCoins(
			sdk.NewInt64Coin("aordinary", 1),
			sdk.NewInt64Coin("bordinary", 1),
		),
	)
	testApp := &App{txConfig: encoding.TxConfig}

	for _, maxGas := range []int64{0, -1} {
		t.Run(standardMsgSendProposalMaxGasName(maxGas), func(t *testing.T) {
			innerCalls := 0
			handler := testApp.standardMsgSendProcessProposal(func(
				_ sdk.Context,
				_ *abci.RequestProcessProposal,
			) (*abci.ResponseProcessProposal, error) {
				innerCalls++
				return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
			})

			accepted, err := handler(
				standardMsgSendProposalContext(maxGas),
				&abci.RequestProcessProposal{Txs: standardMsgSendProposalRepeat(candidateBytes, 1024)},
			)
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, accepted.Status)
			require.Equal(t, 1, innerCalls)

			accepted, err = handler(
				standardMsgSendProposalContext(maxGas),
				&abci.RequestProcessProposal{Txs: append(standardMsgSendProposalRepeat(candidateBytes, 1024), ordinaryBytes)},
			)
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, accepted.Status)
			require.Equal(t, 2, innerCalls)
		})
	}
}

func TestStandardMsgSendProcessProposalRejectsUndecodableTx(t *testing.T) {
	encoding := standardMsgSendProposalTestEncoding()
	testApp := &App{txConfig: encoding.TxConfig}
	innerCalls := 0
	handler := testApp.standardMsgSendProcessProposal(func(
		_ sdk.Context,
		_ *abci.RequestProcessProposal,
	) (*abci.ResponseProcessProposal, error) {
		innerCalls++
		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
	})

	for _, maxGas := range []int64{30_000_000, 0, -1} {
		response, err := handler(
			standardMsgSendProposalContext(maxGas),
			&abci.RequestProcessProposal{Txs: [][]byte{{0xff, 0x00}}},
		)
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, response.Status)
		require.Zero(t, innerCalls)
	}
}

func standardMsgSendProposalTestEncoding() appparams.EncodingConfig {
	encoding := appparams.MakeEncodingConfig(
		appparams.Bech32PrefixAccAddr,
		appparams.Bech32PrefixValAddr,
		appparams.Bech32PrefixConsAddr,
	)
	banktypes.RegisterInterfaces(encoding.InterfaceRegistry)
	return encoding
}

func standardMsgSendProposalTestTx(
	t *testing.T,
	encoding appparams.EncodingConfig,
	gas uint64,
	amount sdk.Coins,
) (sdk.Tx, []byte) {
	t.Helper()

	builder := encoding.TxConfig.NewTxBuilder()
	msg := banktypes.NewMsgSend(
		sdk.AccAddress(make([]byte, 20)),
		sdk.AccAddress(append([]byte{1}, make([]byte, 19)...)),
		amount,
	)
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(gas)

	txBytes, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	tx, err := encoding.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	return tx, txBytes
}

func standardMsgSendProposalContext(maxGas int64) sdk.Context {
	return sdk.Context{}.WithConsensusParams(cmtproto.ConsensusParams{
		Block: &cmtproto.BlockParams{MaxGas: maxGas},
	})
}

func standardMsgSendProposalRepeat(txBytes []byte, count int) [][]byte {
	txs := make([][]byte, count)
	for i := range txs {
		txs[i] = txBytes
	}
	return txs
}

func standardMsgSendProposalMaxGasName(maxGas int64) string {
	if maxGas < 0 {
		return "negative_one"
	}
	return "zero"
}
