package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Chain   ChainConfig   `mapstructure:"chain"`
	Keyring KeyringConfig `mapstructure:"keyring"`
	Gas     GasConfig     `mapstructure:"gas"`
}

type ChainConfig struct {
	ChainID  string `mapstructure:"chain_id"`
	Endpoint string `mapstructure:"endpoint"`
}

type KeyringConfig struct {
	Backend    string `mapstructure:"backend"`
	Name       string `mapstructure:"name"`
	Passphrase string `mapstructure:"passphrase"`
}

type GasConfig struct {
	Limit      uint64  `mapstructure:"limit"`
	Adjustment float64 `mapstructure:"adjustment"`
	Denom      string  `mapstructure:"denom"`
}

func LoadFile(path string) (*Config, error) {
	if st, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", os.ErrNotExist, path)
		}
		return nil, fmt.Errorf("stat config file: %w", err)
	} else if st.IsDir() {
		return nil, fmt.Errorf("config path is a directory: %s", path)
	}

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

	// Basic validation (keep minimal; deeper validation happens in daemon).
	if cfg.Chain.ChainID == "" {
		return nil, fmt.Errorf("chain.chain_id is required")
	}
	if cfg.Chain.Endpoint == "" {
		return nil, fmt.Errorf("chain.endpoint is required")
	}
	if cfg.Keyring.Name == "" {
		return nil, fmt.Errorf("keyring.name is required")
	}
	if cfg.Keyring.Backend == "" {
		return nil, fmt.Errorf("keyring.backend is required")
	}
	if cfg.Gas.Limit == 0 {
		return nil, fmt.Errorf("gas.limit must be > 0")
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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(path, defaultConfig, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
