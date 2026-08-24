package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidatePairFeedSourceBoundaries(t *testing.T) {
	t.Parallel()
	t.Run("feed count", func(t *testing.T) {
		for _, test := range []struct {
			count   int
			wantErr bool
		}{
			{count: 256},
			{count: 257, wantErr: true},
		} {
			t.Run(fmt.Sprintf("%d", test.count), func(t *testing.T) {
				cfg, sources, sourceBytes := validPairForOwnershipTest(t)
				sources.Feeds = make([]FeedSource, test.count)
				for i := range sources.Feeds {
					sources.Feeds[i] = ownershipTestFeed(fmt.Sprintf("ASSET/%03d", i), 3)
				}
				err := validatePair(&cfg, &sources, sourceBytes)
				if (err != nil) != test.wantErr {
					t.Fatalf("validatePair feed count %d error = %v, wantErr %t", test.count, err, test.wantErr)
				}
				if test.wantErr && !strings.Contains(err.Error(), "feeds exceed maximum 256") {
					t.Fatalf("validatePair feed count %d error = %v", test.count, err)
				}
			})
		}
	})

	t.Run("source count", func(t *testing.T) {
		for _, test := range []struct {
			count   int
			wantErr bool
		}{
			{count: 2, wantErr: true},
			{count: 3},
			{count: 64},
			{count: 65, wantErr: true},
		} {
			t.Run(fmt.Sprintf("%d", test.count), func(t *testing.T) {
				cfg, sources, sourceBytes := validPairForOwnershipTest(t)
				sources.Feeds = []FeedSource{ownershipTestFeed("BTC/USD", test.count)}
				err := validatePair(&cfg, &sources, sourceBytes)
				if (err != nil) != test.wantErr {
					t.Fatalf("validatePair source count %d error = %v, wantErr %t", test.count, err, test.wantErr)
				}
				if test.wantErr && !strings.Contains(err.Error(), `feed "BTC/USD" must have 3-64 sources`) {
					t.Fatalf("validatePair source count %d error = %v", test.count, err)
				}
			})
		}
	})

	t.Run("source id", func(t *testing.T) {
		for _, test := range []struct {
			name    string
			value   string
			wantErr bool
		}{
			{name: "empty", wantErr: true},
			{name: "minimum", value: "a"},
			{name: "maximum", value: strings.Repeat("a", 128)},
			{name: "too long", value: strings.Repeat("a", 129), wantErr: true},
			{name: "allowed alphabet", value: "AZaz09._-"},
			{name: "space", value: "a b", wantErr: true},
			{name: "slash", value: "a/b", wantErr: true},
			{name: "non ascii", value: "가격", wantErr: true},
			{name: "other punctuation", value: "a:b", wantErr: true},
		} {
			t.Run(test.name, func(t *testing.T) {
				cfg, sources, sourceBytes := validPairForOwnershipTest(t)
				feed := ownershipTestFeed("BTC/USD", 3)
				feed.Sources[0].ID = test.value
				sources.Feeds = []FeedSource{feed}
				err := validatePair(&cfg, &sources, sourceBytes)
				if (err != nil) != test.wantErr {
					t.Fatalf("validatePair source id %q error = %v, wantErr %t", test.value, err, test.wantErr)
				}
				if test.wantErr && !strings.Contains(err.Error(), `feed "BTC/USD" source 0 has invalid id`) {
					t.Fatalf("validatePair source id %q error = %v", test.value, err)
				}
			})
		}
	})
}

func TestHistoryRetentionBoundaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value   uint32
		wantErr bool
	}{
		{value: 0, wantErr: true},
		{value: 1},
		{value: 1000},
		{value: 1001, wantErr: true},
	} {
		t.Run(fmt.Sprintf("%d", test.value), func(t *testing.T) {
			cfg, _, _ := validPairForOwnershipTest(t)
			cfg.Storage.HistoryRetention = test.value
			err := validateResourceBounds(&cfg)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateResourceBounds retention %d error = %v, wantErr %t", test.value, err, test.wantErr)
			}
			if test.wantErr && err.Error() != "storage.history_retention must be between 1 and 1000" {
				t.Fatalf("validateResourceBounds retention %d error = %v", test.value, err)
			}
		})
	}
}

func TestPrepareInitialHomeCreatesOnlyPrivateManagedDirectories(t *testing.T) {
	t.Parallel()
	base := canonicalTemp(t)
	ancestor := filepath.Join(base, "ancestor")
	if err := os.Mkdir(ancestor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ancestor, 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(ancestor, "home")
	absolute, err := PrepareInitialHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if absolute != home {
		t.Fatalf("absolute home = %q, want %q", absolute, home)
	}
	for _, directory := range []string{home, filepath.Join(home, "data"), filepath.Join(home, "logs"), filepath.Join(home, "run")} {
		info, err := os.Lstat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("managed directory %q mode = %v", directory, info.Mode())
		}
	}
	ancestorInfo, err := os.Lstat(ancestor)
	if err != nil {
		t.Fatal(err)
	}
	if ancestorInfo.Mode().Perm() != 0o755 {
		t.Fatalf("ancestor mode changed to %o", ancestorInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	if strings.Join(names, ",") != "data,logs,run" {
		t.Fatalf("managed home entries = %v", names)
	}
	assertOwnershipFilesAbsent(t, home, "")
	for _, directory := range []string{"data", "logs", "run"} {
		entries, err := os.ReadDir(filepath.Join(home, directory))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("managed directory %q contains entries: %v", directory, entries)
		}
	}
}

func TestPrepareInitialHomeRejectsUnsafeManagedDirectoriesWithoutRepair(t *testing.T) {
	t.Parallel()
	for _, relative := range []string{".", "data", "logs", "run"} {
		t.Run(relative, func(t *testing.T) {
			home := filepath.Join(canonicalTemp(t), "home")
			if _, err := PrepareInitialHome(home); err != nil {
				t.Fatal(err)
			}
			target := home
			if relative != "." {
				target = filepath.Join(home, relative)
			}
			if err := os.Chmod(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareInitialHome(home); err == nil || !strings.Contains(err.Error(), "permissions must be 0700") {
				t.Fatalf("PrepareInitialHome(%q) error = %v", relative, err)
			}
			info, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o755 {
				t.Fatalf("unsafe directory %q mode changed to %o", relative, info.Mode().Perm())
			}
			assertOwnershipFilesAbsent(t, home, "")
		})
	}
}

func TestPrepareInitialHomeRejectsSymlinkAndNonDirectoryComponents(t *testing.T) {
	t.Parallel()
	t.Run("ancestor symlink", func(t *testing.T) {
		base := canonicalTemp(t)
		realParent := filepath.Join(base, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}
		linkParent := filepath.Join(base, "link")
		if err := os.Symlink(realParent, linkParent); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareInitialHome(filepath.Join(linkParent, "home")); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("PrepareInitialHome ancestor symlink error = %v", err)
		}
	})
	t.Run("managed symlink", func(t *testing.T) {
		base := canonicalTemp(t)
		home := filepath.Join(base, "home")
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(base, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(home, "data")); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareInitialHome(home); err == nil || !strings.Contains(err.Error(), "safe directory") {
			t.Fatalf("PrepareInitialHome managed symlink error = %v", err)
		}
	})
	t.Run("managed regular file", func(t *testing.T) {
		home := filepath.Join(canonicalTemp(t), "home")
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "data"), []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareInitialHome(home); err == nil || !strings.Contains(err.Error(), "safe directory") {
			t.Fatalf("PrepareInitialHome managed file error = %v", err)
		}
	})
}

func TestWriteInitialFilesRefusesProtectedTargetOverwrite(t *testing.T) {
	t.Parallel()
	for _, relative := range []string{
		"config.toml",
		"sources.toml",
		"data/oracle.db",
		"data/storage.meta",
		"run/oracle.sock",
		"run/admin.sock",
	} {
		t.Run(relative, func(t *testing.T) {
			home := filepath.Join(canonicalTemp(t), "home")
			if _, err := PrepareInitialHome(home); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, filepath.FromSlash(relative))
			if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := WriteInitialFiles(home); err == nil || !strings.Contains(err.Error(), "refusing to overwrite existing") {
				t.Fatalf("WriteInitialFiles protected target %q error = %v", relative, err)
			}
			content, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "sentinel" {
				t.Fatalf("protected target %q changed to %q", relative, content)
			}
			assertOwnershipFilesAbsent(t, home, relative)
		})
	}
}

func validPairForOwnershipTest(t *testing.T) (File, SourcesFile, []byte) {
	t.Helper()
	sourceBytes := []byte("ownership validation")
	digest := sha256.Sum256(sourceBytes)
	var cfg File
	if err := strictDecode([]byte(initialConfigTemplate("revision", hex.EncodeToString(digest[:]))), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, SourcesFile{SchemaVersion: SchemaVersion, PublicationRevision: "revision"}, sourceBytes
}

func ownershipTestFeed(symbol string, sourceCount int) FeedSource {
	feed := FeedSource{
		Symbol:     symbol,
		Interval:   Duration{Duration: 10 * time.Second},
		StaleAfter: Duration{Duration: 10 * time.Second},
		Sources:    make([]SourceConfig, sourceCount),
	}
	for i := range feed.Sources {
		feed.Sources[i] = SourceConfig{
			ID:          fmt.Sprintf("source-%03d", i),
			URL:         fmt.Sprintf("https://source-%03d.example/value", i),
			JSONPointer: "/value",
		}
	}
	return feed
}

func assertOwnershipFilesAbsent(t *testing.T, home, except string) {
	t.Helper()
	for _, relative := range []string{
		"config.toml",
		"sources.toml",
		"data/oracle.db",
		"data/storage.meta",
		"run/oracle.sock",
		"run/admin.sock",
		"run/oracled.lock",
	} {
		if relative == except {
			continue
		}
		if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(relative))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected file %q: %v", relative, err)
		}
	}
}
