package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type ChainIDConfig struct {
	CosmosChainID uint64
	EVMChainID    uint64
}

var ChainIDMapping = map[string]ChainIDConfig{
	"guru_630-1": {CosmosChainID: 630, EVMChainID: 630},
	"guru_631-1": {CosmosChainID: 631, EVMChainID: 631},
}

func GetChainIDs(chainID string) (cosmosChainID uint64, evmChainID uint64, exists bool) {
	if chainID == "" {
		return 0, 0, false
	}
	cfg, ok := ChainIDMapping[chainID]
	if !ok {
		return 0, 0, false
	}
	return cfg.CosmosChainID, cfg.EVMChainID, true
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

	if cosmosID, evmID, ok := GetChainIDs(chainID); ok {
		EVMChainID = evmID
		GuruChainID = cosmosID
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

	EVMChainID = evmID
	GuruChainID = evmID
	return nil
}
