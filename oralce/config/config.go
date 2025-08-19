// Package config provides configuration management for the Oracle daemon
// It handles loading, validation, and access to all configuration parameters
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/GPTx-global/guru-v2/crypto/hd"
	"github.com/GPTx-global/guru-v2/encoding"
	guruconfig "github.com/GPTx-global/guru-v2/server/config"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pelletier/go-toml/v2"
)

// Command line flag for oracle daemon home directory
var home = flag.String("home", homeDir(), "oracle daemon home directory")

var (
	// Global configuration data structure
	globalConfig configData
	// Mutex for thread-safe access to configuration
	mu sync.Mutex
)

// configData represents the complete configuration structure
type configData struct {
	Chain chainConfig `toml:"chain"`
	Key   keyConfig   `toml:"key"`
	Gas   gasConfig   `toml:"gas"`
	Retry retryConfig `toml:"retry"`
	HTTP  httpConfig  `toml:"http"`
}

// chainConfig contains blockchain network configuration
type chainConfig struct {
	ID       string `toml:"id"`
	Endpoint string `toml:"endpoint"`
}

// keyConfig contains cryptographic key and keyring configuration
type keyConfig struct {
	Name           string `toml:"name"`
	KeyringDir     string `toml:"keyring_dir"`
	KeyringBackend string `toml:"keyring_backend"`
}

// gasConfig contains transaction gas configuration
type gasConfig struct {
	Limit      uint64  `toml:"limit"`
	Adjustment float64 `toml:"adjustment"`
	Prices     string  `toml:"prices"`
}

// retryConfig contains retry logic and circuit breaker configuration
type retryConfig struct {
	MaxAttempts       int `toml:"max_attempts"`
	InitialBackoffSec int `toml:"initial_backoff_sec"`
	MaxBackoffSec     int `toml:"max_backoff_sec"`
	CBFailures        int `toml:"circuit_breaker_failures"`
	CBWindowSec       int `toml:"circuit_breaker_window_sec"`
	CBCooldownSec     int `toml:"circuit_breaker_cooldown_sec"`
}

// httpConfig contains HTTP client configuration for external data fetching
type httpConfig struct {
	TimeoutSec            int  `toml:"timeout_sec"`
	MaxIdleConns          int  `toml:"max_idle_conns"`
	MaxIdlePerHost        int  `toml:"max_idle_per_host"`
	MaxConnsPerHost       int  `toml:"max_conns_per_host"`
	ReadBufferKB          int  `toml:"read_buffer_kb"`
	WriteBufferKB         int  `toml:"write_buffer_kb"`
	RequestsPerSec        int  `toml:"requests_per_sec"`
	IdleConnTimeout       int  `toml:"idle_conn_timeout_sec"`
	DisableKeepAlives     bool `toml:"disable_keep_alives"`
	DisableCompression    bool `toml:"disable_compression"`
	ForceAttemptHTTP2     bool `toml:"force_attempt_http2"`
	TLSHandshakeTimeout   int  `toml:"tls_handshake_timeout_sec"`
	ExpectContinueTimeout int  `toml:"expect_continue_timeout_sec"`
}

// Load reads and parses the configuration file from the home directory
// If the configuration file does not exist, it creates a default one
// This function panics on any configuration errors to prevent daemon startup with invalid config
func Load() {
	path := filepath.Join(Home(), "config.toml")

	// Create default config file if it doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := createDefaultConfig(path); err != nil {
			panic(fmt.Sprintf("Failed to create default config: %v", err))
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("Failed to read config file: %v", err))
	}

	// Parse TOML configuration
	if err := toml.Unmarshal(data, &globalConfig); err != nil {
		panic(fmt.Sprintf("Failed to parse TOML: %v", err))
	}

	// Validate configuration values
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
	// Ensure the configuration directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Set default configuration values
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
			Prices:     "630000000000aguru",
		},
		Retry: retryConfig{
			MaxAttempts:       6,
			InitialBackoffSec: 1,
			MaxBackoffSec:     8,
			CBFailures:        5,
			CBWindowSec:       30,
			CBCooldownSec:     30,
		},
		HTTP: httpConfig{
			TimeoutSec:            30,
			MaxIdleConns:          1000,
			MaxIdlePerHost:        100,
			MaxConnsPerHost:       200,
			ReadBufferKB:          32,
			WriteBufferKB:         32,
			RequestsPerSec:        20,
			IdleConnTimeout:       90,
			DisableKeepAlives:     false,
			DisableCompression:    false,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10,
			ExpectContinueTimeout: 1,
		},
	}

	data, err := toml.Marshal(globalConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal TOML: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validateConfig checks configuration values for correctness and completeness
// Sets default values for optional parameters and validates required ones
func validateConfig() error {
	// Validate required chain configuration
	if globalConfig.Chain.ID == "" {
		return fmt.Errorf("chain ID is required")
	}

	if globalConfig.Chain.Endpoint == "" {
		return fmt.Errorf("chain endpoint is required")
	}

	// Validate required key configuration
	if globalConfig.Key.Name == "" {
		return fmt.Errorf("key name is required")
	}

	if globalConfig.Key.KeyringDir == "" {
		return fmt.Errorf("keyring directory is required")
	}

	if globalConfig.Key.KeyringBackend == "" {
		return fmt.Errorf("keyring backend is required")
	}

	// Validate required gas configuration
	if globalConfig.Gas.Limit == 0 {
		return fmt.Errorf("gas limit is required")
	}

	if globalConfig.Gas.Adjustment <= 0 {
		globalConfig.Gas.Adjustment = 1.2
	}

	if globalConfig.Gas.Prices == "" {
		return fmt.Errorf("gas prices is required")
	}

	// set sane defaults if omitted
	if globalConfig.Retry.MaxAttempts <= 0 {
		globalConfig.Retry.MaxAttempts = 6
	}
	if globalConfig.Retry.InitialBackoffSec <= 0 {
		globalConfig.Retry.InitialBackoffSec = 1
	}
	if globalConfig.Retry.MaxBackoffSec <= 0 {
		globalConfig.Retry.MaxBackoffSec = 8
	}
	if globalConfig.HTTP.TimeoutSec <= 0 {
		globalConfig.HTTP.TimeoutSec = 30
	}
	if globalConfig.HTTP.MaxIdleConns <= 0 {
		globalConfig.HTTP.MaxIdleConns = 1000
	}
	if globalConfig.HTTP.MaxIdlePerHost <= 0 {
		globalConfig.HTTP.MaxIdlePerHost = 100
	}
	if globalConfig.HTTP.MaxConnsPerHost <= 0 {
		globalConfig.HTTP.MaxConnsPerHost = 200
	}
	if globalConfig.HTTP.ReadBufferKB <= 0 {
		globalConfig.HTTP.ReadBufferKB = 32
	}
	if globalConfig.HTTP.WriteBufferKB <= 0 {
		globalConfig.HTTP.WriteBufferKB = 32
	}
	if globalConfig.HTTP.RequestsPerSec <= 0 {
		globalConfig.HTTP.RequestsPerSec = 20
	}
	if globalConfig.Retry.CBFailures <= 0 {
		globalConfig.Retry.CBFailures = 5
	}
	if globalConfig.Retry.CBWindowSec <= 0 {
		globalConfig.Retry.CBWindowSec = 30
	}
	if globalConfig.Retry.CBCooldownSec <= 0 {
		globalConfig.Retry.CBCooldownSec = 30
	}
	if globalConfig.HTTP.IdleConnTimeout <= 0 {
		globalConfig.HTTP.IdleConnTimeout = 90
	}
	// if globalConfig.HTTP.DisableKeepAlives {
	// 	globalConfig.HTTP.DisableKeepAlives = false
	// }
	// if globalConfig.HTTP.DisableCompression {
	// 	globalConfig.HTTP.DisableCompression = false
	// }
	// if globalConfig.HTTP.ForceAttemptHTTP2 {
	// 	globalConfig.HTTP.ForceAttemptHTTP2 = true
	// }
	if globalConfig.HTTP.TLSHandshakeTimeout <= 0 {
		globalConfig.HTTP.TLSHandshakeTimeout = 10
	}
	if globalConfig.HTTP.ExpectContinueTimeout <= 0 {
		globalConfig.HTTP.ExpectContinueTimeout = 1
	}

	return nil
}

// Keyring creates and returns a keyring instance based on the configuration
// Supports test, file, and OS keyring backends with EthSecp256k1 option
func Keyring() keyring.Keyring {
	encCfg := encoding.MakeConfig(guruconfig.DefaultEVMChainID)

	// Map configuration backend to keyring backend constant
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

	// Get key information from keyring
	info, err := kr.Key(KeyName())
	if err != nil {
		panic(fmt.Sprintf("Failed to get key info: %v", err))
	}

	// Extract address from key
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

// Configuration getter functions provide read-only access to configuration values

// Home returns the Oracle daemon home directory path
func Home() string { return *home }

// Chain configuration getters
func ChainID() string       { return globalConfig.Chain.ID }
func ChainEndpoint() string { return globalConfig.Chain.Endpoint }

// Key configuration getters
func KeyName() string        { return globalConfig.Key.Name }
func KeyringDir() string     { return globalConfig.Key.KeyringDir }
func KeyringBackend() string { return globalConfig.Key.KeyringBackend }

// Gas configuration getters
func GasLimit() uint64       { return globalConfig.Gas.Limit }
func GasAdjustment() float64 { return globalConfig.Gas.Adjustment }

// Channel size for event subscriptions
func ChannelSize() int { return 1 << 10 }

// Retry configuration getters
func RetryMaxAttempts() int       { return globalConfig.Retry.MaxAttempts }
func RetryInitialBackoffSec() int { return globalConfig.Retry.InitialBackoffSec }
func RetryMaxBackoffSec() int     { return globalConfig.Retry.MaxBackoffSec }
func RetryCBFailures() int        { return globalConfig.Retry.CBFailures }
func RetryCBWindowSec() int       { return globalConfig.Retry.CBWindowSec }
func RetryCBCooldownSec() int     { return globalConfig.Retry.CBCooldownSec }

// HTTP configuration getters
func HTTPTimeoutSec() int                { return globalConfig.HTTP.TimeoutSec }
func HTTPMaxIdleConns() int              { return globalConfig.HTTP.MaxIdleConns }
func HTTPMaxIdlePerHost() int            { return globalConfig.HTTP.MaxIdlePerHost }
func HTTPMaxConnsPerHost() int           { return globalConfig.HTTP.MaxConnsPerHost }
func HTTPReadBufferKB() int              { return globalConfig.HTTP.ReadBufferKB * 1024 }
func HTTPWriteBufferKB() int             { return globalConfig.HTTP.WriteBufferKB * 1024 }
func HTTPRequestsPerSec() int            { return globalConfig.HTTP.RequestsPerSec }
func HTTPIdleConnTimeoutSec() int        { return globalConfig.HTTP.IdleConnTimeout }
func HTTPDisableKeepAlives() bool        { return globalConfig.HTTP.DisableKeepAlives }
func HTTPDisableCompression() bool       { return globalConfig.HTTP.DisableCompression }
func HTTPForceAttemptHTTP2() bool        { return globalConfig.HTTP.ForceAttemptHTTP2 }
func HTTPTLSHandshakeTimeoutSec() int    { return globalConfig.HTTP.TLSHandshakeTimeout }
func HTTPEExpectContinueTimeoutSec() int { return globalConfig.HTTP.ExpectContinueTimeout }
