package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
)

// RegisterInterfaces registers oracle Msg/MsgResponse types without relying on
// msgservice.RegisterMsgServiceDesc, which expects gogo descriptor metadata.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&oraclev1.MsgUpdateParams{},
		&oraclev1.MsgUpdateOracleTask{},
		&oraclev1.MsgUpdatePrices{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&oraclev1.MsgUpdateParamsResponse{},
		&oraclev1.MsgUpdateOracleTaskResponse{},
		&oraclev1.MsgUpdatePricesResponse{},
	)
}
