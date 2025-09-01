package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/GPTx-global/guru-v2/crypto/hd"
	"github.com/GPTx-global/guru-v2/encoding"
	guruconfig "github.com/GPTx-global/guru-v2/server/config"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pelletier/go-toml/v2"
)

// Config는 Oracle daemon의 모든 설정을 포함하는 불변 구조체
type Config struct {
	// 설정 파일 경로
	configPath string

	// 설정 데이터
	Chain  ChainConfig  `toml:"chain"`
	Key    KeyConfig    `toml:"key"`
	Gas    GasConfig    `toml:"gas"`
	Retry  RetryConfig  `toml:"retry"`
	Worker WorkerConfig `toml:"worker"`

	// 런타임 객체 (TOML에 저장되지 않음)
	keyring keyring.Keyring
	address sdk.AccAddress
	mu      *sync.RWMutex
}

// ChainConfig는 블록체인 연결 설정
type ChainConfig struct {
	ID       string `toml:"id" comment:"Chain ID (e.g., guru_631-1)"`
	Endpoint string `toml:"endpoint" comment:"RPC endpoint URL"`
}

// KeyConfig는 키 관리 설정
type KeyConfig struct {
	Name           string `toml:"name" comment:"Key name in keyring"`
	KeyringDir     string `toml:"keyring_dir" comment:"Keyring directory path"`
	KeyringBackend string `toml:"keyring_backend" comment:"Keyring backend (test, file, os)"`
}

// GasConfig는 트랜잭션 가스 설정
type GasConfig struct {
	Limit      uint64  `toml:"limit" comment:"Gas limit per transaction"`
	Adjustment float64 `toml:"adjustment" comment:"Gas adjustment factor"`
	Prices     string  `toml:"prices" comment:"Gas prices (without denom)"`
}

// RetryConfig는 재시도 정책 설정
type RetryConfig struct {
	MaxAttempts int `toml:"max_attempts" comment:"Maximum retry attempts"`
	MaxDelaySec int `toml:"max_delay_sec" comment:"Maximum delay between retries (seconds)"`
}

// WorkerConfig는 워커 풀 설정
type WorkerConfig struct {
	PoolSize    int `toml:"pool_size" comment:"Number of concurrent workers"`
	ChannelSize int `toml:"channel_size" comment:"Event channel buffer size"`
	Timeout     int `toml:"timeout_sec" comment:"HTTP request timeout (seconds)"`
}

// 기본 설정값
var defaultConfig = Config{
	Chain: ChainConfig{
		ID:       "guru_631-1",
		Endpoint: "http://localhost:26657",
	},
	Key: KeyConfig{
		Name:           "mykey",
		KeyringBackend: "test",
	},
	Gas: GasConfig{
		Limit:      70000,
		Adjustment: 1.5,
		Prices:     "630000000000",
	},
	Retry: RetryConfig{
		MaxAttempts: 4,
		MaxDelaySec: 10,
	},
	Worker: WorkerConfig{
		PoolSize:    4,
		ChannelSize: 1024,
		Timeout:     30,
	},
}

// LoadConfig는 설정을 로드하거나 기본 설정 파일을 생성
func LoadConfig(homeDir string) (*Config, error) {
	if homeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		homeDir = filepath.Join(home, ".oracled")
	}

	configPath := filepath.Join(homeDir, "config.toml")

	// 설정 파일이 존재하는지 확인
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 설정 파일이 없으면 기본 설정으로 생성
		cfg := defaultConfig
		cfg.configPath = configPath
		cfg.Key.KeyringDir = homeDir
		cfg.mu = &sync.RWMutex{}

		if err := cfg.save(); err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}

		fmt.Printf("Created default configuration at %s\n", configPath)
		fmt.Println("Please review and update the configuration as needed.")

		return &cfg, nil
	}

	// 설정 파일 읽기
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// TOML 파싱
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.configPath = configPath
	cfg.mu = &sync.RWMutex{}

	// 설정 검증 및 기본값 적용
	if err := cfg.validateAndSetDefaults(homeDir); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// 런타임 객체 초기화
	if err := cfg.initializeRuntime(); err != nil {
		return nil, fmt.Errorf("failed to initialize runtime objects: %w", err)
	}

	return &cfg, nil
}

// save는 현재 설정을 파일에 저장
func (c *Config) save() error {
	// 디렉토리 생성
	dir := filepath.Dir(c.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// TOML로 마샬링
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 파일에 쓰기
	if err := os.WriteFile(c.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validateAndSetDefaults는 설정을 검증하고 기본값 적용
func (c *Config) validateAndSetDefaults(homeDir string) error {
	// Chain 설정 검증
	if c.Chain.ID == "" {
		return fmt.Errorf("chain.id is required")
	}
	if c.Chain.Endpoint == "" {
		return fmt.Errorf("chain.endpoint is required")
	}

	// Key 설정 검증
	if c.Key.Name == "" {
		return fmt.Errorf("key.name is required")
	}
	if c.Key.KeyringDir == "" {
		c.Key.KeyringDir = homeDir
	}
	if c.Key.KeyringBackend == "" {
		c.Key.KeyringBackend = "test"
	}
	switch c.Key.KeyringBackend {
	case "test", "file", "os":
		// 유효한 백엔드
	default:
		return fmt.Errorf("invalid keyring backend: %s (must be test, file, or os)", c.Key.KeyringBackend)
	}

	// Gas 설정 검증
	if c.Gas.Limit == 0 {
		c.Gas.Limit = defaultConfig.Gas.Limit
	}
	if c.Gas.Adjustment <= 0 {
		c.Gas.Adjustment = defaultConfig.Gas.Adjustment
	}
	if c.Gas.Prices == "" {
		return fmt.Errorf("gas.prices is required")
	}

	// Retry 설정 검증
	if c.Retry.MaxAttempts <= 0 {
		c.Retry.MaxAttempts = defaultConfig.Retry.MaxAttempts
	}
	if c.Retry.MaxDelaySec <= 0 {
		c.Retry.MaxDelaySec = defaultConfig.Retry.MaxDelaySec
	}

	// Worker 설정 검증
	if c.Worker.PoolSize <= 0 {
		c.Worker.PoolSize = defaultConfig.Worker.PoolSize
	}
	if c.Worker.ChannelSize <= 0 {
		c.Worker.ChannelSize = defaultConfig.Worker.ChannelSize
	}
	if c.Worker.Timeout <= 0 {
		c.Worker.Timeout = defaultConfig.Worker.Timeout
	}

	return nil
}

// initializeRuntime은 런타임 객체 초기화
func (c *Config) initializeRuntime() error {
	// Keyring 생성
	encCfg := encoding.MakeConfig(guruconfig.DefaultEVMChainID)

	var backend string
	switch c.Key.KeyringBackend {
	case "test":
		backend = keyring.BackendTest
	case "file":
		backend = keyring.BackendFile
	case "os":
		backend = keyring.BackendOS
	}

	kr, err := keyring.New(
		"guru",
		backend,
		c.Key.KeyringDir,
		nil,
		encCfg.Codec,
		hd.EthSecp256k1Option(),
	)
	if err != nil {
		return fmt.Errorf("failed to create keyring: %w", err)
	}

	// 키 정보 조회
	info, err := kr.Key(c.Key.Name)
	if err != nil {
		return fmt.Errorf("failed to get key info for '%s': %w", c.Key.Name, err)
	}

	// 주소 추출
	address, err := info.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get address: %w", err)
	}

	c.keyring = kr
	c.address = address

	return nil
}

// Clone은 설정의 복사본을 생성 (불변성 보장)
func (c *Config) Clone() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := Config{
		configPath: c.configPath,
		Chain:      c.Chain,
		Key:        c.Key,
		Gas:        c.Gas,
		Retry:      c.Retry,
		Worker:     c.Worker,
		keyring:    c.keyring,
		address:    c.address,
		mu:         &sync.RWMutex{},
	}
	return &clone
}

// UpdateGasPrice는 가스 가격을 업데이트한 새 설정 반환
func (c *Config) UpdateGasPrice(newPrice string) *Config {
	c.mu.Lock()
	defer c.mu.Unlock()

	clone := c.Clone()
	clone.Gas.Prices = newPrice
	return clone
}

// Getter 메서드들
func (c *Config) ChainID() string        { return c.Chain.ID }
func (c *Config) ChainEndpoint() string  { return c.Chain.Endpoint }
func (c *Config) KeyName() string        { return c.Key.Name }
func (c *Config) KeyringDir() string     { return c.Key.KeyringDir }
func (c *Config) KeyringBackend() string { return c.Key.KeyringBackend }
func (c *Config) GasLimit() uint64       { return c.Gas.Limit }
func (c *Config) GasAdjustment() float64 { return c.Gas.Adjustment }
func (c *Config) GasPrices() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Gas.Prices
}

func (c *Config) RetryMaxAttempts() int { return c.Retry.MaxAttempts }
func (c *Config) RetryMaxDelay() time.Duration {
	return time.Duration(c.Retry.MaxDelaySec) * time.Second
}

func (c *Config) WorkerPoolSize() int    { return c.Worker.PoolSize }
func (c *Config) WorkerChannelSize() int { return c.Worker.ChannelSize }
func (c *Config) WorkerTimeout() time.Duration {
	return time.Duration(c.Worker.Timeout) * time.Second
}

// Runtime 객체 접근
func (c *Config) Keyring() keyring.Keyring {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.keyring
}

func (c *Config) Address() sdk.AccAddress {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.address
}

// String은 설정을 문자열로 표현 (민감한 정보 제외)
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{Chain:%s, Address:%s, Workers:%d, RetryAttempts:%d}",
		c.Chain.ID,
		c.address.String(),
		c.Worker.PoolSize,
		c.Retry.MaxAttempts,
	)
}

// ReloadRuntime은 런타임 객체를 다시 초기화 (키 변경 시 필요)
func (c *Config) ReloadRuntime() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.initializeRuntime()
}

// GetConfigPath는 설정 파일 경로 반환
func (c *Config) GetConfigPath() string {
	return c.configPath
}
