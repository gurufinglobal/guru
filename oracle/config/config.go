package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Home    string
	Chain   ChainConfig
	Keyring KeyringConfig
	Gas     GasConfig
}

type ChainConfig struct {
	ChainID  string `toml:"chain_id"`
	Endpoint string `toml:"endpoint"`
}

type KeyringConfig struct {
	Backend    string `toml:"backend"`
	Name       string `toml:"name"`
	Passphrase string `toml:"passphrase"`
}

type GasConfig struct {
	Limit      uint64  `toml:"limit"`
	Adjustment float64 `toml:"adjustment"`
	Denom      string  `toml:"denom"`
}

func LoadFile(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

func WriteDefaultFile(path string) error {
	defaultConfig := []byte(`# Oracle Daemon Configuration

[chain]
chain_id = "guru_631-1"
endpoint = "http://localhost:26657"

[keyring]
name = "oracle_feeder"
backend = "test"
passphrase = "password"

[gas]
limit = 70000
adjustment = 1.5
denom = "agxn"
`)

	if err := os.WriteFile(path, defaultConfig, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
