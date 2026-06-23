package params

import (
	"fmt"
	"os"
	"strconv"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	DisplayDenom = "gxn"
	BaseDenom    = "a" + DisplayDenom // atto-gxn
)

const (
	AppName    = "guru"
	EVMChainID = uint64(631)
	EnvName    = AppName + "d"
	HomeDir    = "." + EnvName
)

var (
	SDKChainID = AppName + "_" + strconv.Itoa(int(EVMChainID))
)

func MustDefaultHomeDir() string {
	defaultNodeHome, err := clienthelpers.GetNodeHomeDirectory(HomeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting default home directory:", err)
		os.Exit(1)
	}
	return defaultNodeHome
}

const (
	Bech32Prefix         = AppName
	Bech32PrefixAccAddr  = Bech32Prefix
	Bech32PrefixAccPub   = Bech32PrefixAccAddr + sdk.PrefixPublic
	Bech32PrefixValAddr  = Bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator
	Bech32PrefixValPub   = Bech32PrefixValAddr + sdk.PrefixPublic
	Bech32PrefixConsAddr = Bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus
	Bech32PrefixConsPub  = Bech32PrefixConsAddr + sdk.PrefixPublic
)
