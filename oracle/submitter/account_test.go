package submitter

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc"
)

type mockAuthClient struct {
	resp *authtypes.QueryAccountInfoResponse
	err  error
}

func (m mockAuthClient) AccountInfo(ctx context.Context, in *authtypes.QueryAccountInfoRequest, _ ...grpc.CallOption) (*authtypes.QueryAccountInfoResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

func TestAccountInfo_ResetAccountInfo_SetsAccountAndSequence(t *testing.T) {
	t.Parallel()

	addr := sdk.AccAddress(make([]byte, 20))
	ai := NewAccountInfo(mockAuthClient{
		resp: &authtypes.QueryAccountInfoResponse{
			Info: &authtypes.BaseAccount{
				AccountNumber: 9,
				Sequence:      12,
			},
		},
	}, addr)

	if err := ai.ResetAccountInfo(context.Background()); err != nil {
		t.Fatalf("ResetAccountInfo error: %v", err)
	}
	if got := ai.AccountNumber(); got != 9 {
		t.Fatalf("expected account_number=9, got %d", got)
	}
	if got := ai.CurrentSequenceNumber(); got != 12 {
		t.Fatalf("expected sequence=12, got %d", got)
	}
}

func TestAccountInfo_ResetAccountInfo_ReturnsError(t *testing.T) {
	t.Parallel()

	addr := sdk.AccAddress(make([]byte, 20))
	ai := NewAccountInfo(mockAuthClient{err: errors.New("boom")}, addr)

	if err := ai.ResetAccountInfo(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}
