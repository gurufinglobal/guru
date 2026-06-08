package oracle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oracled.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
socket = "/tmp/guru-oracle.sock"
request_timeout = "1s"
source_timeout = "250ms"

[[sources]]
name = "source-a"
symbol = "BTC/USD"
value_type = "NUMERIC"
url = "http://127.0.0.1:8080/btc"
response_path = "data.price"
timeout = "100ms"
interval = "1s"
`), 0o600))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "/tmp/guru-oracle.sock", cfg.Socket)
	require.Len(t, cfg.Sources, 1)
	require.Equal(t, "source-a", cfg.Sources[0].Name)
	require.Equal(t, "1s", cfg.Sources[0].Interval)
}

func TestConfigValidateRejectsDuplicateSourceNameForSymbol(t *testing.T) {
	cfg := Config{
		Socket:           "/tmp/guru-oracle.sock",
		RequestTimeout:   "1s",
		SourceTimeout:    "250ms",
		NodeGRPC:         "127.0.0.1:9090",
		NodeQueryTimeout: "1s",
		Sources: []SourceConfig{
			{Name: "source-a", Symbol: "BTC/USD", URL: "http://example.invalid/a", ResponsePath: "price"},
			{Name: "source-a", Symbol: "btc/usd", URL: "http://example.invalid/b", ResponsePath: "price"},
		},
	}

	require.ErrorContains(t, cfg.Validate(), "duplicate source name")
}
