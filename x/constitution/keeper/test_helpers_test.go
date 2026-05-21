package keeper

import (
	"errors"
	"testing"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type failingCodec struct{}

func (failingCodec) StringToBytes(string) ([]byte, error) {
	return nil, errors.New("string to bytes failed")
}

func (failingCodec) BytesToString([]byte) (string, error) {
	return "", errors.New("bytes to string failed")
}

func mustInt(t *testing.T, amount string) math.Int {
	t.Helper()

	value, ok := math.NewIntFromString(amount)
	if !ok {
		t.Fatalf("failed to parse int: %s", amount)
	}

	return value
}

func mustAnyWithValue(t *testing.T, msg sdk.Msg) *codectypes.Any {
	t.Helper()

	any, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		t.Fatalf("failed to pack message into Any: %v", err)
	}

	return any
}
