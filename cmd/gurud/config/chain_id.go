package config

import (
	"path/filepath"

	"github.com/spf13/viper"

	"github.com/cosmos/cosmos-sdk/client/config"
)

// ChainID defines the chain ID for the Guru blockchain.
// It serves as the single source of truth for both EVM compatibility (EIP-155)
// and Cosmos SDK encoding/configuration needs.
var ChainID = uint64(631)

// GetChainID returns the current chain ID
func GetChainID() uint64 {
	return ChainID
}

// SetChainID sets the chain ID for all components
func SetChainID(id uint64) {
	ChainID = id
}

// GetChainIDFromHome returns the chain ID from the client configuration
// in the given home directory.
func GetChainIDFromHome(home string) (string, error) {
	v := viper.New()
	v.AddConfigPath(filepath.Join(home, "config"))
	v.SetConfigName("client")
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		return "", err
	}
	conf := new(config.ClientConfig)

	if err := v.Unmarshal(conf); err != nil {
		return "", err
	}

	return conf.ChainID, nil
}
