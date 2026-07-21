package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCloneMessageDeepCopiesInternalGogoMessage(t *testing.T) {
	original := &Exchange{
		Id:       7,
		Metadata: map[string]string{"market": "GURU/USD"},
	}

	cloned := CloneMessage(original)
	require.True(t, EqualMessages(original, cloned))
	require.NotSame(t, original, cloned)

	cloned.Metadata["market"] = "ATOM/USD"
	require.Equal(t, "GURU/USD", original.GetMetadata()["market"])
	require.False(t, EqualMessages(original, cloned))

	var nilExchange *Exchange
	require.Nil(t, CloneMessage(nilExchange))

	ledger := &FeeLedger{Coins: sdk.NewCoins(sdk.NewInt64Coin("agxn", 10))}
	clonedLedger := CloneMessage(ledger)
	require.True(t, EqualMessages(ledger, clonedLedger))
	require.NotSame(t, ledger, clonedLedger)
}

func TestEqualMessagesUsesProto3NilEmptySemantics(t *testing.T) {
	require.True(t, EqualMessages(
		&Exchange{},
		&Exchange{Metadata: map[string]string{}},
	))
	require.True(t, EqualMessages(
		&FeeLedger{},
		&FeeLedger{Coins: sdk.Coins{}},
	))
	require.False(t, EqualMessages(
		&Exchange{},
		&Exchange{Metadata: map[string]string{"tier": "one"}},
	))
}

func TestInternalWrapperConstructors(t *testing.T) {
	patch := &ExchangeUpdatePatch{
		NewAdminAddress:                   NewStringValue("new-admin"),
		FeeBpsAToB:                        NewUInt32Value(9),
		PendingVolumeEpochEffectiveAtUnix: NewUInt64Value(123),
		ClearMetadata:                     NewBoolValue(true),
	}

	require.Equal(t, "new-admin", patch.GetNewAdminAddress().GetValue())
	require.Equal(t, uint32(9), patch.GetFeeBpsAToB().GetValue())
	require.Equal(t, uint64(123), patch.GetPendingVolumeEpochEffectiveAtUnix().GetValue())
	require.True(t, patch.GetClearMetadata().GetValue())
}

func TestInternalMapMessagesMarshalDeterministically(t *testing.T) {
	messages := []interface {
		Marshal() ([]byte, error)
	}{
		&Exchange{Metadata: map[string]string{"zeta": "3", "alpha": "1", "middle": "2"}},
		&ExchangeUpdatePatch{Metadata: map[string]string{"zeta": "3", "alpha": "1", "middle": "2"}},
		&MsgRegisterExchange{Metadata: map[string]string{"zeta": "3", "alpha": "1", "middle": "2"}},
	}

	for _, message := range messages {
		expected, err := message.Marshal()
		require.NoError(t, err)
		for range 100 {
			actual, err := message.Marshal()
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		}
	}
}
