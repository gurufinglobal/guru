package app

import (
	"testing"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	appparams "github.com/gurufinglobal/guru/v3/app/params"
	oracleabci "github.com/gurufinglobal/guru/v3/x/oracle/abci"
	oracletypes "github.com/gurufinglobal/guru/v3/x/oracle/types"
	"github.com/stretchr/testify/require"
)

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
