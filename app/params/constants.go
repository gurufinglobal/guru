package params

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	AppName      = "guru"
	BaseDenom    = "agxn"
	DisplayDenom = "gxn"
)

const (
	Bech32Prefix         = AppName
	Bech32PrefixAccAddr  = Bech32Prefix
	Bech32PrefixAccPub   = Bech32PrefixAccAddr + sdk.PrefixPublic
	Bech32PrefixValAddr  = Bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator
	Bech32PrefixValPub   = Bech32PrefixValAddr + sdk.PrefixPublic
	Bech32PrefixConsAddr = Bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus
	Bech32PrefixConsPub  = Bech32PrefixConsAddr + sdk.PrefixPublic
)
