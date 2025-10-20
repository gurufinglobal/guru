package gurud

import (
	cmn "github.com/gurufinglobal/guru/v2/precompiles/common"
	evmtypes "github.com/gurufinglobal/guru/v2/x/vm/types"
)

type BankKeeper interface {
	evmtypes.BankKeeper
	cmn.BankKeeper
}
