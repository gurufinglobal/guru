package ante

import (
	feepolicytypes "github.com/GPTx-global/guru-v2/v2/x/feepolicy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type FeePolicyKeeper interface {
	GetDiscount(ctx sdk.Context, feePayerAddr string, msgs []sdk.Msg) feepolicytypes.Discount
}
