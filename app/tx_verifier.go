package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ baseapp.ProposalTxVerifier = &NoCheckProposalTxVerifier{}

type NoCheckProposalTxVerifier struct {
	*baseapp.BaseApp
}

func NewNoCheckProposalTxVerifier(b *baseapp.BaseApp) *NoCheckProposalTxVerifier {
	return &NoCheckProposalTxVerifier{BaseApp: b}
}

// PrepareProposalVerifyTx skips running ante checks and only verifies tx encoding.
func (txv *NoCheckProposalTxVerifier) PrepareProposalVerifyTx(tx sdk.Tx) ([]byte, error) {
	return txv.TxEncode(tx)
}
