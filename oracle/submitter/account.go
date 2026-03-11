package submitter

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

type AuthClient interface {
	AccountInfo(ctx context.Context, in *authtypes.QueryAccountInfoRequest, opts ...grpc.CallOption) (*authtypes.QueryAccountInfoResponse, error)
}

type AccountInfo struct {
	authClient     AuthClient
	address        sdk.AccAddress
	accountNumber  uint64
	sequenceNumber uint64
}

func NewAccountInfo(authClient AuthClient, address sdk.AccAddress) *AccountInfo {
	return &AccountInfo{
		authClient:     authClient,
		address:        address,
		accountNumber:  0,
		sequenceNumber: 0,
	}
}

func (a *AccountInfo) AccountNumber() uint64 {
	return atomic.LoadUint64(&a.accountNumber)
}

func (a *AccountInfo) CurrentSequenceNumber() uint64 {
	return atomic.LoadUint64(&a.sequenceNumber)
}

func (a *AccountInfo) IncrementSequenceNumber() {
	atomic.AddUint64(&a.sequenceNumber, 1)
}

func (a *AccountInfo) ResetAccountInfo(ctx context.Context) error {
	// Use a bounded timeout even if caller context is long-lived.
	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	accInfo, err := a.authClient.AccountInfo(subCtx, &authtypes.QueryAccountInfoRequest{Address: a.address.String()})
	if err != nil {
		return fmt.Errorf("query account info: %w", err)
	}

	atomic.StoreUint64(&a.accountNumber, accInfo.Info.AccountNumber)
	atomic.StoreUint64(&a.sequenceNumber, accInfo.Info.Sequence)
	return nil
}
