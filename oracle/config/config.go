package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Chain ChainConfig
}

type ChainConfig struct {
	RPCAddr       string
	GRPCAddr      string
	ChainID       string
	KeyName       string
	KeyringDir    string
	KeyPassphrase string
	GasAdjustment float64
	GasPrices     string
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
rpc_addr = "tcp://localhost:26657"
grpc_addr = "localhost:9090"
chain_id = "guru-1"
key_name = "oracle_feeder"
keyring_dir = "~/.guru/keyring-test"
key_passphrase = "password"
gas_adjustment = 1.5
gas_prices = "0.025uguru"
`)

	return os.WriteFile(path, defaultConfig, 0644)
}
