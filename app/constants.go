package app

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	appName   = "guru"
	BaseDenom = "agxn"
)

const (
	Bech32Prefix         = appName
	Bech32PrefixAccAddr  = Bech32Prefix
	Bech32PrefixAccPub   = Bech32PrefixAccAddr + sdk.PrefixPublic
	Bech32PrefixValAddr  = Bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator
	Bech32PrefixValPub   = Bech32PrefixValAddr + sdk.PrefixPublic
	Bech32PrefixConsAddr = Bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus
	Bech32PrefixConsPub  = Bech32PrefixConsAddr + sdk.PrefixPublic
)
