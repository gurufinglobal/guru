package gurud

import (
	cmn "github.com/GPTx-global/guru-v2/precompiles/common"
	evmtypes "github.com/GPTx-global/guru-v2/x/vm/types"
)

type BankKeeper interface {
	evmtypes.BankKeeper
	cmn.BankKeeper
}
