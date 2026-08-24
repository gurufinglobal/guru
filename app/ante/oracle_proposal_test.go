package ante

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	oracletypes "github.com/gurufinglobal/guru/v2/x/oracle/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func TestOracleProposalOptionIsOnlyAcceptedDuringFinalize(t *testing.T) {
	option := &codectypes.Any{TypeUrl: oracletypes.ProposalPayloadTypeURL}
	tx := extensionTx{critical: []*codectypes.Any{option}}
	nextCalled := false
	handler := WrapAnteHandlerWithOracleProposalOptionBlock(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	finalizeContext, err := handler(sdk.Context{}.WithExecMode(sdk.ExecModeFinalize), tx, false)
	require.NoError(t, err)
	require.False(t, nextCalled)
	require.Equal(t, uint64(0), finalizeContext.GasMeter().Limit())

	_, err = handler(sdk.Context{}.WithExecMode(sdk.ExecModeCheck), tx, false)
	require.ErrorIs(t, err, sdkerrors.ErrUnknownExtensionOptions)
}

func TestOracleProposalOptionRejectsNonCanonicalExtensionLayout(t *testing.T) {
	option := &codectypes.Any{TypeUrl: oracletypes.ProposalPayloadTypeURL}
	handler := WrapAnteHandlerWithOracleProposalOptionBlock(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	ctx := sdk.Context{}.WithExecMode(sdk.ExecModeFinalize)

	_, err := handler(ctx, extensionTx{nonCritical: []*codectypes.Any{option}}, false)
	require.ErrorIs(t, err, sdkerrors.ErrUnknownExtensionOptions)

	_, err = handler(ctx, extensionTx{critical: []*codectypes.Any{option, option}}, false)
	require.ErrorIs(t, err, sdkerrors.ErrUnknownExtensionOptions)
}

type extensionTx struct {
	critical    []*codectypes.Any
	nonCritical []*codectypes.Any
}

func (extensionTx) GetMsgs() []sdk.Msg { return nil }

func (extensionTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx extensionTx) GetExtensionOptions() []*codectypes.Any { return tx.critical }

func (tx extensionTx) GetNonCriticalExtensionOptions() []*codectypes.Any {
	return tx.nonCritical
}
