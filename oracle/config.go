package oracle

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"github.com/spf13/viper"
)

const (
	defaultRequestTimeout = "2s"
	defaultSourceTimeout  = "500ms"
	defaultNodeGRPC       = "127.0.0.1:9090"
	defaultNodeQueryTime  = "2s"
)

type Config struct {
	Socket           string         `mapstructure:"socket"`
	RequestTimeout   string         `mapstructure:"request_timeout"`
	SourceTimeout    string         `mapstructure:"source_timeout"`
	NodeGRPC         string         `mapstructure:"node_grpc"`
	NodeQueryTimeout string         `mapstructure:"node_query_timeout"`
	Sources          []SourceConfig `mapstructure:"sources"`
}

type SourceConfig struct {
	Name         string            `mapstructure:"name"`
	Symbol       string            `mapstructure:"symbol"`
	ValueType    string            `mapstructure:"value_type"`
	URL          string            `mapstructure:"url"`
	ResponsePath string            `mapstructure:"response_path"`
	Timeout      string            `mapstructure:"timeout"`
	Interval     string            `mapstructure:"interval"`
	Headers      map[string]string `mapstructure:"headers"`
}

func DefaultConfig(homeDir string) Config {
	return Config{
		Socket:           filepath.Join(homeDir, "oracle", "oracle.sock"),
		RequestTimeout:   defaultRequestTimeout,
		SourceTimeout:    defaultSourceTimeout,
		NodeGRPC:         defaultNodeGRPC,
		NodeQueryTimeout: defaultNodeQueryTime,
	}
}

func LoadConfig(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults("")

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c *Config) applyDefaults(homeDir string) {
	defaults := DefaultConfig(homeDir)
	if strings.TrimSpace(c.Socket) == "" {
		c.Socket = defaults.Socket
	}
	if strings.TrimSpace(c.RequestTimeout) == "" {
		c.RequestTimeout = defaultRequestTimeout
	}
	if strings.TrimSpace(c.SourceTimeout) == "" {
		c.SourceTimeout = defaultSourceTimeout
	}
	if strings.TrimSpace(c.NodeGRPC) == "" {
		c.NodeGRPC = defaultNodeGRPC
	}
	if strings.TrimSpace(c.NodeQueryTimeout) == "" {
		c.NodeQueryTimeout = defaultNodeQueryTime
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Socket) == "" {
		return fmt.Errorf("socket is required")
	}
	if _, err := c.RequestTimeoutDuration(); err != nil {
		return fmt.Errorf("request_timeout: %w", err)
	}
	if _, err := c.SourceTimeoutDuration(); err != nil {
		return fmt.Errorf("source_timeout: %w", err)
	}
	if strings.TrimSpace(c.NodeGRPC) == "" {
		return fmt.Errorf("node_grpc is required")
	}
	if _, err := c.NodeQueryTimeoutDuration(); err != nil {
		return fmt.Errorf("node_query_timeout: %w", err)
	}

	seen := map[string]struct{}{}
	for i, source := range c.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("sources[%d]: %w", i, err)
		}
		key := normalizeSymbol(source.Symbol) + "\x00" + strings.TrimSpace(source.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("sources[%d]: duplicate source name %q for symbol %q", i, source.Name, source.Symbol)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func (c Config) RequestTimeoutDuration() (time.Duration, error) {
	return parseDuration(c.RequestTimeout)
}

func (c Config) SourceTimeoutDuration() (time.Duration, error) {
	return parseDuration(c.SourceTimeout)
}

func (c Config) NodeQueryTimeoutDuration() (time.Duration, error) {
	return parseDuration(c.NodeQueryTimeout)
}

func (s SourceConfig) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(s.Symbol) == "" {
		return fmt.Errorf("symbol is required")
	}
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("url is required")
	}
	if strings.TrimSpace(s.ResponsePath) == "" {
		return fmt.Errorf("response_path is required")
	}
	if _, err := s.ProtoValueType(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Timeout) != "" {
		if _, err := parseDuration(s.Timeout); err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
	}
	if strings.TrimSpace(s.Interval) != "" {
		if _, err := parseDuration(s.Interval); err != nil {
			return fmt.Errorf("interval: %w", err)
		}
	}

	return nil
}

func (s SourceConfig) IntervalDuration() (time.Duration, bool, error) {
	if strings.TrimSpace(s.Interval) == "" {
		return 0, false, nil
	}
	duration, err := parseDuration(s.Interval)
	if err != nil {
		return 0, false, err
	}

	return duration, true, nil
}

func (s SourceConfig) ProtoValueType() (oraclev1.ValueType, error) {
	return ParseValueType(s.ValueType)
}

func ParseValueType(value string) (oraclev1.ValueType, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "NUMERIC", "VALUE_TYPE_NUMERIC":
		return oraclev1.ValueType_VALUE_TYPE_NUMERIC, nil
	case "STRING", "VALUE_TYPE_STRING":
		return oraclev1.ValueType_VALUE_TYPE_STRING, nil
	case "BOOL", "BOOLEAN", "VALUE_TYPE_BOOL":
		return oraclev1.ValueType_VALUE_TYPE_BOOL, nil
	default:
		return oraclev1.ValueType_VALUE_TYPE_UNSPECIFIED, fmt.Errorf("unsupported value_type %q", value)
	}
}

func parseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, fmt.Errorf("must be positive")
	}

	return duration, nil
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}
