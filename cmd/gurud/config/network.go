package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type ChainIDConfig struct {
	// ChainID is the single numeric chain ID (used for both EVM and Cosmos contexts).
	ChainID uint64
}

var ChainIDMapping = map[string]ChainIDConfig{
	"guru_630-1": {ChainID: 630},
	"guru_631-1": {ChainID: 631},
}

func GetChainIDFromString(chainID string) (id uint64, exists bool) {
	if chainID == "" {
		return 0, false
	}
	cfg, ok := ChainIDMapping[chainID]
	if !ok {
		return 0, false
	}
	return cfg.ChainID, true
}

func LoadChainIDsFromConfig(homeDir string) error {
	clientTomlPath := filepath.Join(homeDir, "config", "client.toml")
	if _, err := os.Stat(clientTomlPath); os.IsNotExist(err) {
		// Some commands (e.g. keys) can run before init; keep defaults.
		return nil
	}

	chainID, err := GetChainIDFromHome(homeDir)
	if err != nil {
		// Don't block non-start commands if client.toml can't be parsed.
		return nil
	}
	if chainID == "" {
		// chain-id may be intentionally unset before init; keep defaults.
		return nil
	}

	if id, ok := GetChainIDFromString(chainID); ok {
		// Use the same chain ID for both EVM and Cosmos contexts
		// In current setup, they are the same value
		SetChainID(id)
		return nil
	}

	appTomlPath := filepath.Join(homeDir, "config", "app.toml")
	if _, err := os.Stat(appTomlPath); os.IsNotExist(err) {
		return nil
	}

	v := viper.New()
	v.SetConfigFile(appTomlPath)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		return nil
	}

	evmID := v.GetUint64("evm.evm-chain-id")
	if evmID == 0 {
		return nil
	}

	SetChainID(evmID)
	return nil
}
