package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestWriteInitialAndLoadEmptyPair(t *testing.T) {
	t.Parallel()
	home := filepath.Join(canonicalTemp(t), "home")
	paths, err := WriteInitialFiles(home)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if pair.Paths != paths {
		t.Fatalf("paths differ:\n%+v\n%+v", pair.Paths, paths)
	}
	if pair.Feeds == nil || len(pair.Feeds) != 0 {
		t.Fatalf("initial feeds = %#v", pair.Feeds)
	}
	wantPolicy := domain.CollectorPolicy{
		MaxConcurrency:        pair.Config.Collector.MaxConcurrency,
		SourceResponseBytes:   pair.Config.Collector.SourceResponseBytes,
		MaxRedirects:          pair.Config.Collector.MaxRedirects,
		MaxAttempts:           pair.Config.Collector.MaxAttempts,
		RequestTimeout:        pair.Config.Collector.RequestTimeout.Duration,
		ConnectTimeout:        pair.Config.Collector.ConnectTimeout.Duration,
		TLSHandshakeTimeout:   pair.Config.Collector.TLSHandshakeTimeout.Duration,
		ResponseHeaderTimeout: pair.Config.Collector.ResponseHeaderTimeout.Duration,
		RetryInitialBackoff:   pair.Config.Collector.RetryInitialBackoff.Duration,
		RetryMaxBackoff:       pair.Config.Collector.RetryMaxBackoff.Duration,
	}
	if pair.CollectorPolicy != wantPolicy {
		t.Fatalf("collector policy = %#v, want %#v", pair.CollectorPolicy, wantPolicy)
	}
	for _, path := range []string{paths.ConfigFile, paths.SourcesFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", path, info.Mode().Perm())
		}
	}
	if _, err := WriteInitialFiles(home); err == nil {
		t.Fatal("second initialization unexpectedly succeeded")
	}
}

func TestWriteInitialFilesDoesNotChmodExistingDirectory(t *testing.T) {
	t.Parallel()
	base := canonicalTemp(t)
	home := filepath.Join(base, "shared")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteInitialFiles(home); err == nil ||
		!strings.Contains(err.Error(), "permissions must be 0700") {
		t.Fatalf("WriteInitialFiles error = %v", err)
	}
	info, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing directory permissions changed to %o", info.Mode().Perm())
	}
}

func TestLoadRejectsUnknownFieldAndDigestMismatch(t *testing.T) {
	t.Parallel()
	t.Run("unknown config", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		paths, err := WriteInitialFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		content = append(content, []byte("\nunknown = true\n")...)
		if err := os.WriteFile(paths.ConfigFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "strict mode") {
			t.Fatalf("Load() error = %v", err)
		}
	})
	t.Run("sources digest", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		paths, err := WriteInitialFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(paths.SourcesFile, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.WriteString("\n")
		_ = file.Close()
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
			t.Fatalf("Load() error = %v", err)
		}
	})
}

func TestLoadValidatesFeedAndCanonicalizesPlan(t *testing.T) {
	t.Parallel()
	home := filepath.Join(canonicalTemp(t), "home")
	paths, err := WriteInitialFiles(home)
	if err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	revision := extractQuoted(t, string(configBytes), "publication_revision")
	sources := []byte(`schema_version = 1
publication_revision = "` + revision + `"

[[feeds]]
symbol = "BTC/USD"
interval = "1s"
stale_after = "2s"

[[feeds.sources]]
id = "a"
url = "https://a.example/value"
json_pointer = "/data/price"

[[feeds.sources]]
id = "b"
url = "https://b.example/value"
json_pointer = "/data/price"

[[feeds.sources]]
id = "c"
url = "https://c.example/value"
json_pointer = "/data/price"
`)
	sum := sha256.Sum256(sources)
	oldDigest := extractQuoted(t, string(configBytes), "sources_sha256")
	configBytes = []byte(strings.Replace(string(configBytes), oldDigest, hex.EncodeToString(sum[:]), 1))
	if err := os.WriteFile(paths.SourcesFile, sources, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Feeds) != 1 || pair.Feeds[0].Symbol != "BTC/USD" || len(pair.Feeds[0].Sources) != 3 {
		t.Fatalf("unexpected plans: %#v", pair.Feeds)
	}
	if pair.Feeds[0].Fingerprint == ([32]byte{}) || pair.PlanDigest == ([32]byte{}) {
		t.Fatal("plan fingerprint was not populated")
	}

	sources = []byte(strings.Replace(string(sources), `id = "c"`, `id = "b"`, 1))
	sum = sha256.Sum256(sources)
	configBytes = []byte(strings.Replace(string(configBytes), hex.EncodeToString(pairDigestBytes(t, pair.Config.SourcesSHA256)), hex.EncodeToString(sum[:]), 1))
	if err := os.WriteFile(paths.SourcesFile, sources, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "duplicate source id") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsSymlinkedHomeAndUnsafePath(t *testing.T) {
	t.Parallel()
	base := canonicalTemp(t)
	home := filepath.Join(base, "real")
	paths, err := WriteInitialFiles(home)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Load(symlink) error = %v", err)
	}

	content, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), `consumer_socket = "run/oracle.sock"`, `consumer_socket = "../oracle.sock"`, 1))
	if err := os.WriteFile(paths.ConfigFile, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "escapes home") {
		t.Fatalf("Load(unsafe path) error = %v", err)
	}
}

func TestLoadRejectsReservedTargetsAndInsecureDirectories(t *testing.T) {
	t.Parallel()
	t.Run("reserved config target", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		paths, err := WriteInitialFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		content = []byte(strings.Replace(
			string(content),
			`lock = "run/oracled.lock"`,
			`lock = "config.toml"`,
			1,
		))
		if err := os.WriteFile(paths.ConfigFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "aliases config.toml") {
			t.Fatalf("Load(reserved target) error = %v", err)
		}
	})
	t.Run("reserved directory target", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		paths, err := WriteInitialFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		content = []byte(strings.Replace(
			string(content),
			`database = "data/oracle.db"`,
			`database = "data"`,
			1,
		))
		if err := os.WriteFile(paths.ConfigFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "aliases data directory") {
			t.Fatalf("Load(reserved directory) error = %v", err)
		}
	})
	t.Run("insecure run directory", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		if _, err := WriteInitialFiles(home); err != nil {
			t.Fatal(err)
		}
		runDirectory := filepath.Join(home, "run")
		if err := os.Chmod(runDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "permissions must be 0700") {
			t.Fatalf("Load(insecure directory) error = %v", err)
		}
	})
	t.Run("split storage pair", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		paths, err := WriteInitialFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		content = []byte(strings.Replace(
			string(content),
			`marker = "data/storage.meta"`,
			`marker = "run/storage.meta"`,
			1,
		))
		if err := os.WriteFile(paths.ConfigFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "must share a directory") {
			t.Fatalf("Load(split storage pair) error = %v", err)
		}
	})
	t.Run("mutable home lock path", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		paths, err := WriteInitialFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(paths.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		content = []byte(strings.Replace(
			string(content),
			`lock = "run/oracled.lock"`,
			`lock = "run/alternate.lock"`,
			1,
		))
		if err := os.WriteFile(paths.ConfigFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(home); err == nil || !strings.Contains(err.Error(), `must equal "run/oracled.lock"`) {
			t.Fatalf("Load(mutable lock path) error = %v", err)
		}
	})
}

func TestCanonicalHomeLockPathDoesNotReadMutableConfiguration(t *testing.T) {
	t.Parallel()
	home := filepath.Join(canonicalTemp(t), "home")
	paths, err := WriteInitialFiles(home)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := CanonicalHomeLockPath(home)
	if err != nil {
		t.Fatal(err)
	}
	if lockPath != paths.Lock {
		t.Fatalf("lock path = %q, want %q", lockPath, paths.Lock)
	}

	if err := os.Chmod(filepath.Join(home, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalHomeLockPath(home); err == nil ||
		!strings.Contains(err.Error(), "permissions must be 0700") {
		t.Fatalf("CanonicalHomeLockPath(insecure run) error = %v", err)
	}
}

func TestReadBoundedRegularRejectsUnsafeFiles(t *testing.T) {
	t.Parallel()
	directory := canonicalTemp(t)
	private := filepath.Join(directory, "private.toml")
	if err := os.WriteFile(private, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0o400); err != nil {
		t.Fatal(err)
	}
	if data, err := readBoundedRegular(private, 16); err != nil || string(data) != "value" {
		t.Fatalf("read-only private file: data=%q err=%v", data, err)
	}

	public := filepath.Join(directory, "public.toml")
	if err := os.WriteFile(public, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(public, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegular(public, 16); err == nil ||
		!strings.Contains(err.Error(), "permissions are not private") {
		t.Fatalf("public file error = %v", err)
	}

	link := filepath.Join(directory, "link.toml")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegular(link, 16); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestConsumerProtocolLimitsAreFixed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		request  uint32
		response uint32
		want     string
	}{
		{name: "request", request: (64 << 10) - 1, response: 1 << 20, want: "must equal 65536"},
		{name: "response", request: 64 << 10, response: (1 << 20) - 1, want: "must equal 1048576"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &File{Server: Server{
				MaxRequestBytes:  test.request,
				MaxResponseBytes: test.response,
			}}
			if err := validateResourceBounds(cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateResourceBounds error = %v", err)
			}
		})
	}
}

func extractQuoted(t *testing.T, content, key string) string {
	t.Helper()
	prefix := key + ` = "`
	start := strings.Index(content, prefix)
	if start < 0 {
		t.Fatalf("%s missing", key)
	}
	start += len(prefix)
	end := strings.IndexByte(content[start:], '"')
	if end < 0 {
		t.Fatalf("%s closing quote missing", key)
	}
	return content[start : start+end]
}

func pairDigestBytes(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
