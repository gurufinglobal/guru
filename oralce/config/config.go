package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gurufinglobal/guru/v2/crypto/hd"
	"github.com/gurufinglobal/guru/v2/encoding"
	guruconfig "github.com/gurufinglobal/guru/v2/server/config"
	"github.com/pelletier/go-toml/v2"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var home = flag.String("home", homeDir(), "oracle daemon home directory")

var (
	globalConfig configData
	mu           sync.Mutex
)

type configData struct {
	Chain chainConfig `toml:"chain"`
	Key   keyConfig   `toml:"key"`
	Gas   gasConfig   `toml:"gas"`
	Retry retryConfig `toml:"retry"`
}

type chainConfig struct {
	ID       string `toml:"id"`
	Endpoint string `toml:"endpoint"`
}

type keyConfig struct {
	Name           string `toml:"name"`
	KeyringDir     string `toml:"keyring_dir"`
	KeyringBackend string `toml:"keyring_backend"`
}

type gasConfig struct {
	Limit      uint64  `toml:"limit"`
	Adjustment float64 `toml:"adjustment"`
	Prices     string  `toml:"prices"`
}

type retryConfig struct {
	MaxAttempts int `toml:"max_attempts"`
	MaxDelaySec int `toml:"max_delay_sec"`
}

// Load reads and parses the configuration file from the home directory
// If the configuration file does not exist, it creates a default one
// This function panics on any configuration errors to prevent daemon startup with invalid config
func Load() {
	path := filepath.Join(Home(), "config.toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := createDefaultConfig(path); err != nil {
			panic(fmt.Sprintf("Failed to create default config: %v", err))
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("Failed to read config file: %v", err))
	}

	if err := toml.Unmarshal(data, &globalConfig); err != nil {
		panic(fmt.Sprintf("Failed to parse TOML: %v", err))
	}

	if err := validateConfig(); err != nil {
		panic(fmt.Sprintf("Invalid config: %v", err))
	}

	fmt.Printf("Loaded config from %s\n", path)
}

// homeDir returns the Oracle daemon home directory path
// Defaults to ~/.oracled in the user's home directory
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("Failed to get user home directory: %v", err))
	}

	return filepath.Join(home, ".oracled")
}

// createDefaultConfig generates and writes a default configuration file
// Used when no configuration file exists on first startup
func createDefaultConfig(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	globalConfig = configData{
		Chain: chainConfig{
			ID:       "guru_631-1",
			Endpoint: "http://localhost:26657",
		},
		Key: keyConfig{
			Name:           "mykey",
			KeyringDir:     Home(),
			KeyringBackend: "test",
		},
		Gas: gasConfig{
			Limit:      70000,
			Adjustment: 1.5,
			Prices:     "630000000000",
		},
		Retry: retryConfig{
			MaxAttempts: 4,
			MaxDelaySec: 10,
		},
	}

	data, err := toml.Marshal(globalConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal TOML: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validateConfig checks configuration values for correctness and completeness
// Sets default values for optional parameters and validates required ones
func validateConfig() error {
	if globalConfig.Chain.ID == "" {
		return fmt.Errorf("chain ID is required")
	}

	if globalConfig.Chain.Endpoint == "" {
		return fmt.Errorf("chain endpoint is required")
	}

	if globalConfig.Key.Name == "" {
		return fmt.Errorf("key name is required")
	}

	if globalConfig.Key.KeyringDir == "" {
		return fmt.Errorf("keyring directory is required")
	}

	if globalConfig.Key.KeyringBackend == "" {
		return fmt.Errorf("keyring backend is required")
	}

	if globalConfig.Gas.Limit == 0 {
		return fmt.Errorf("gas limit is required")
	}

	if globalConfig.Gas.Adjustment <= 0 {
		globalConfig.Gas.Adjustment = 1.2
	}

	if globalConfig.Gas.Prices == "" {
		return fmt.Errorf("gas prices is required")
	}

	if globalConfig.Retry.MaxAttempts <= 0 {
		globalConfig.Retry.MaxAttempts = 6
	}
	if globalConfig.Retry.MaxDelaySec <= 0 {
		globalConfig.Retry.MaxDelaySec = 8
	}

	return nil
}

// Keyring creates and returns a keyring instance based on the configuration
// Supports test, file, and OS keyring backends with EthSecp256k1 option
func Keyring() keyring.Keyring {
	encCfg := encoding.MakeConfig(guruconfig.DefaultEVMChainID)

	var backend string
	switch KeyringBackend() {
	case "test":
		backend = keyring.BackendTest
	case "file":
		backend = keyring.BackendFile
	case "os":
		backend = keyring.BackendOS
	default:
		panic(fmt.Sprintf("Invalid keyring backend: %s", KeyringBackend()))
	}

	kr, err := keyring.New("guru", backend, KeyringDir(), nil, encCfg.Codec, hd.EthSecp256k1Option())
	if err != nil {
		panic(fmt.Sprintf("Failed to create keyring: %v", err))
	}

	return kr
}

// Address retrieves the account address from the configured key name
// Returns the address that will be used to sign Oracle transactions
func Address() sdk.AccAddress {
	kr := Keyring()

	info, err := kr.Key(KeyName())
	if err != nil {
		panic(fmt.Sprintf("Failed to get key info: %v", err))
	}

	address, err := info.GetAddress()
	if err != nil {
		panic(fmt.Sprintf("Failed to get address: %v", err))
	}

	return address
}

// GasPrices returns the current gas prices configuration with thread safety
// Used by transaction builders to set appropriate gas fees
func GasPrices() string {
	mu.Lock()
	defer mu.Unlock()

	return globalConfig.Gas.Prices
}

// SetGasPrice updates the gas prices configuration with thread safety
// Allows dynamic gas price adjustment based on network conditions
func SetGasPrice(gasPrice string) {
	mu.Lock()
	defer mu.Unlock()

	globalConfig.Gas.Prices = gasPrice
}

func Home() string           { return *home }
func ChainID() string        { return globalConfig.Chain.ID }
func ChainEndpoint() string  { return globalConfig.Chain.Endpoint }
func KeyName() string        { return globalConfig.Key.Name }
func KeyringDir() string     { return globalConfig.Key.KeyringDir }
func KeyringBackend() string { return globalConfig.Key.KeyringBackend }
func GasLimit() uint64       { return globalConfig.Gas.Limit }
func GasAdjustment() float64 { return globalConfig.Gas.Adjustment }
func ChannelSize() int       { return 1 << 10 }
func RetryMaxAttempts() int  { return globalConfig.Retry.MaxAttempts }
func RetryMaxDelaySec() time.Duration {
	return time.Duration(globalConfig.Retry.MaxDelaySec) * time.Second
}

func TestConfig() error {
	globalConfig = configData{
		Chain: chainConfig{
			ID:       "guru_631-1",
			Endpoint: "http://localhost:26657",
		},
		Key: keyConfig{
			Name:           "mykey",
			KeyringDir:     Home(),
			KeyringBackend: "test",
		},
		Gas: gasConfig{
			Limit:      70000,
			Adjustment: 1.5,
			Prices:     "630000000000",
		},
		Retry: retryConfig{
			MaxAttempts: 4,
			MaxDelaySec: 10,
		},
	}

	return nil
}
