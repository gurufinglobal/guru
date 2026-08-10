// Package config defines the immutable product and consensus identity used by
// the Guru application.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmutils "github.com/cosmos/evm/utils"
)

const (
	ProductName = "guru"
	BinaryName  = "gurud"
	BaseAppName = BinaryName
	EnvPrefix   = "GURU"

	DefaultNodeHomeName        = ".gurud"
	LocalChainID               = "guru_631-1"
	EVMChainID          uint64 = 631

	Bech32PrefixAccAddr  = "guru"
	Bech32PrefixAccPub   = "gurupub"
	Bech32PrefixValAddr  = "guruvaloper"
	Bech32PrefixValPub   = "guruvaloperpub"
	Bech32PrefixConsAddr = "guruvalcons"
	Bech32PrefixConsPub  = "guruvalconspub"

	BaseDenom     = "agxn"
	DisplayDenom  = "gxn"
	DenomExponent = 18

	BIP44Purpose  uint32 = 44
	BIP44CoinType uint32 = 60
	BIP44HDPath          = "m/44'/60'/0'/0/0"
)

var (
	errNilSDKConfig = errors.New("SDK config must not be nil")
	setupSDKOnce    sync.Once
	setupSDKErr     error
)

// DefaultNodeHome resolves the node home without filesystem mutation.
func DefaultNodeHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, DefaultNodeHomeName), nil
}

// ConfigureSDK applies Guru address and derivation identity to an unsealed SDK
// configuration, then seals it. Accepting the config explicitly keeps the
// identity contract independently testable without mutating the SDK singleton.
func ConfigureSDK(sdkConfig *sdk.Config) (err error) {
	if sdkConfig == nil {
		return errNilSDKConfig
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("configure SDK: %v", recovered)
		}
	}()

	sdkConfig.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	sdkConfig.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	sdkConfig.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
	sdkConfig.SetPurpose(BIP44Purpose)
	sdkConfig.SetCoinType(BIP44CoinType)
	sdkConfig.SetFullFundraiserPath(BIP44HDPath)
	sdkConfig.Seal()

	return nil
}

// SetupSDKConfig configures the process-wide Cosmos SDK singleton exactly once.
// It must run before transaction encoding is constructed.
func SetupSDKConfig() error {
	setupSDKOnce.Do(func() {
		setupSDKErr = ConfigureSDK(sdk.GetConfig())
		if setupSDKErr == nil {
			// Guru's native staking unit has 18 decimal places. Keeping the SDK's
			// micro-denom default here would inflate consensus power by 10^12.
			sdk.DefaultPowerReduction = evmutils.AttoPowerReduction
			sdk.DefaultBondDenom = BaseDenom
		}
	})

	return setupSDKErr
}
