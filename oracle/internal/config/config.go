package config

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
	toml "github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

const (
	SchemaVersion        = 1
	MaxConfigFileSize    = 1 << 20
	HomeLockRelativePath = "run/oracled.lock"
)

var revisionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return errors.New("duration is empty")
	}
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

type File struct {
	SchemaVersion       uint32    `toml:"schema_version"`
	PublicationRevision string    `toml:"publication_revision"`
	SourcesSHA256       string    `toml:"sources_sha256"`
	Server              Server    `toml:"server"`
	Collector           Collector `toml:"collector"`
	Storage             Storage   `toml:"storage"`
	Runtime             Runtime   `toml:"runtime"`
	Logging             Logging   `toml:"logging"`
}

type Server struct {
	ConsumerSocket   string `toml:"consumer_socket"`
	AdminSocket      string `toml:"admin_socket"`
	MaxRequestBytes  uint32 `toml:"max_request_bytes"`
	MaxResponseBytes uint32 `toml:"max_response_bytes"`
}

type Collector struct {
	MaxConcurrency        uint32   `toml:"max_concurrency"`
	SourceResponseBytes   uint32   `toml:"source_response_bytes"`
	MaxRedirects          uint32   `toml:"max_redirects"`
	MaxAttempts           uint32   `toml:"max_attempts"`
	RequestTimeout        Duration `toml:"request_timeout"`
	ConnectTimeout        Duration `toml:"connect_timeout"`
	TLSHandshakeTimeout   Duration `toml:"tls_handshake_timeout"`
	ResponseHeaderTimeout Duration `toml:"response_header_timeout"`
	RetryInitialBackoff   Duration `toml:"retry_initial_backoff"`
	RetryMaxBackoff       Duration `toml:"retry_max_backoff"`
}

type Storage struct {
	Database         string `toml:"database"`
	Marker           string `toml:"marker"`
	Lock             string `toml:"lock"`
	HistoryRetention uint32 `toml:"history_retention"`
}

type Runtime struct {
	ShutdownTimeout Duration `toml:"shutdown_timeout"`
}

type Logging struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type SourcesFile struct {
	SchemaVersion       uint32       `toml:"schema_version"`
	PublicationRevision string       `toml:"publication_revision"`
	Feeds               []FeedSource `toml:"feeds"`
}

type FeedSource struct {
	Symbol     string         `toml:"symbol"`
	Interval   Duration       `toml:"interval"`
	StaleAfter Duration       `toml:"stale_after"`
	Sources    []SourceConfig `toml:"sources"`
}

type SourceConfig struct {
	ID          string `toml:"id"`
	URL         string `toml:"url"`
	JSONPointer string `toml:"json_pointer"`
}

type Paths struct {
	Home           string
	ConfigFile     string
	SourcesFile    string
	ConsumerSocket string
	AdminSocket    string
	Database       string
	Marker         string
	Lock           string
}

type Pair struct {
	Config          File
	CollectorPolicy domain.CollectorPolicy
	Paths           Paths
	Feeds           []domain.FeedPlan
	PlanDigest      [32]byte
}

func DefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".oracled"), nil
}

// CanonicalHomeLockPath resolves the immutable per-home ownership lock without
// reading mutable configuration.
func CanonicalHomeLockPath(home string) (string, error) {
	absoluteHome, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	if err := RejectSymlinkPath(absoluteHome); err != nil {
		return "", fmt.Errorf("validate home: %w", err)
	}
	for _, directory := range []string{absoluteHome, filepath.Join(absoluteHome, "run")} {
		info, err := os.Lstat(directory)
		if err != nil {
			return "", fmt.Errorf("inspect home lock directory %q: %w", directory, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("%q is not a safe directory", directory)
		}
		if info.Mode().Perm() != 0o700 {
			return "", fmt.Errorf("%q permissions must be 0700", directory)
		}
	}
	return filepath.Join(absoluteHome, filepath.FromSlash(HomeLockRelativePath)), nil
}

func Load(home string) (*Pair, error) {
	absoluteHome, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}
	if err := RejectSymlinkPath(absoluteHome); err != nil {
		return nil, fmt.Errorf("validate home: %w", err)
	}
	configPath := filepath.Join(absoluteHome, "config.toml")
	sourcesPath := filepath.Join(absoluteHome, "sources.toml")
	configBytes, err := readBoundedRegular(configPath, MaxConfigFileSize)
	if err != nil {
		return nil, fmt.Errorf("read config.toml: %w", err)
	}
	sourcesBytes, err := readBoundedRegular(sourcesPath, MaxConfigFileSize)
	if err != nil {
		return nil, fmt.Errorf("read sources.toml: %w", err)
	}

	var cfg File
	if err := strictDecode(configBytes, &cfg); err != nil {
		return nil, fmt.Errorf("decode config.toml: %w", err)
	}
	var sources SourcesFile
	if err := strictDecode(sourcesBytes, &sources); err != nil {
		return nil, fmt.Errorf("decode sources.toml: %w", err)
	}
	if err := validatePair(&cfg, &sources, sourcesBytes); err != nil {
		return nil, err
	}

	paths, err := resolvePaths(absoluteHome, cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePrivateDirectories(paths); err != nil {
		return nil, err
	}
	policy := collectorPolicy(cfg.Collector)
	feeds, digest, err := buildPlans(sources, policy)
	if err != nil {
		return nil, err
	}
	return &Pair{
		Config:          cfg,
		CollectorPolicy: policy,
		Paths:           paths,
		Feeds:           feeds,
		PlanDigest:      digest,
	}, nil
}

func strictDecode(input []byte, target any) error {
	decoder := toml.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func validatePair(cfg *File, sources *SourcesFile, sourceBytes []byte) error {
	if cfg.SchemaVersion != SchemaVersion || sources.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version: config=%d sources=%d", cfg.SchemaVersion, sources.SchemaVersion)
	}
	if len(cfg.PublicationRevision) < 1 || len(cfg.PublicationRevision) > 128 ||
		!revisionPattern.MatchString(cfg.PublicationRevision) {
		return errors.New("publication_revision must be 1-128 safe ASCII characters")
	}
	if sources.PublicationRevision != cfg.PublicationRevision {
		return errors.New("publication revision mismatch")
	}
	if len(cfg.SourcesSHA256) != 64 || strings.ToLower(cfg.SourcesSHA256) != cfg.SourcesSHA256 {
		return errors.New("sources_sha256 must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(cfg.SourcesSHA256); err != nil {
		return errors.New("sources_sha256 is not hexadecimal")
	}
	sum := sha256.Sum256(sourceBytes)
	if hex.EncodeToString(sum[:]) != cfg.SourcesSHA256 {
		return errors.New("sources.toml digest mismatch")
	}
	if len(sources.Feeds) > domain.MaxFeeds {
		return fmt.Errorf("feeds exceed maximum %d", domain.MaxFeeds)
	}
	if err := validateResourceBounds(cfg); err != nil {
		return err
	}

	seenSymbols := make(map[string]struct{}, len(sources.Feeds))
	for i := range sources.Feeds {
		feed := &sources.Feeds[i]
		symbol, err := domain.NormalizeSymbol(feed.Symbol)
		if err != nil {
			return fmt.Errorf("feed %d: %w", i, err)
		}
		if feed.Symbol != symbol {
			return fmt.Errorf("feed %q symbol must be canonical %q", feed.Symbol, symbol)
		}
		if _, exists := seenSymbols[symbol]; exists {
			return fmt.Errorf("duplicate normalized symbol %q", symbol)
		}
		seenSymbols[symbol] = struct{}{}
		if feed.Interval.Duration < time.Second || feed.Interval.Duration > 24*time.Hour {
			return fmt.Errorf("feed %q interval must be between 1s and 24h", symbol)
		}
		if feed.StaleAfter.Duration < feed.Interval.Duration || feed.StaleAfter.Duration > 7*24*time.Hour {
			return fmt.Errorf("feed %q stale_after must be between interval and 168h", symbol)
		}
		if len(feed.Sources) < domain.MinSourcesPerFeed || len(feed.Sources) > domain.MaxSourcesPerFeed {
			return fmt.Errorf("feed %q must have %d-%d sources", symbol, domain.MinSourcesPerFeed, domain.MaxSourcesPerFeed)
		}
		ids := make(map[string]struct{}, len(feed.Sources))
		urls := make(map[string]struct{}, len(feed.Sources))
		for j := range feed.Sources {
			source := &feed.Sources[j]
			if err := domain.ValidateSourceID(source.ID); err != nil {
				return fmt.Errorf("feed %q source %d has invalid id", symbol, j)
			}
			if _, exists := ids[source.ID]; exists {
				return fmt.Errorf("feed %q has duplicate source id %q", symbol, source.ID)
			}
			ids[source.ID] = struct{}{}
			if _, exists := urls[source.URL]; exists {
				return fmt.Errorf("feed %q has duplicate source URL", symbol)
			}
			urls[source.URL] = struct{}{}
			if err := domain.ValidateSourceURL(source.URL); err != nil {
				return fmt.Errorf("feed %q source %q: %w", symbol, source.ID, err)
			}
			if err := domain.ValidateJSONPointer(source.JSONPointer); err != nil {
				return fmt.Errorf("feed %q source %q: %w", symbol, source.ID, err)
			}
		}
	}
	return nil
}

func validateResourceBounds(cfg *File) error {
	if cfg.Server.MaxRequestBytes != 64<<10 {
		return errors.New("server.max_request_bytes must equal 65536")
	}
	if cfg.Server.MaxResponseBytes != 1<<20 {
		return errors.New("server.max_response_bytes must equal 1048576")
	}
	c := cfg.Collector
	if c.MaxConcurrency < 1 || c.MaxConcurrency > 256 {
		return errors.New("collector.max_concurrency must be between 1 and 256")
	}
	if c.SourceResponseBytes < 1 || c.SourceResponseBytes > 16<<20 {
		return errors.New("collector.source_response_bytes must be between 1 and 16777216")
	}
	if c.MaxRedirects > 10 {
		return errors.New("collector.max_redirects must be between 0 and 10")
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 5 {
		return errors.New("collector.max_attempts must be between 1 and 5")
	}
	for _, timeout := range []struct {
		name  string
		value time.Duration
	}{
		{"request_timeout", c.RequestTimeout.Duration},
		{"connect_timeout", c.ConnectTimeout.Duration},
		{"tls_handshake_timeout", c.TLSHandshakeTimeout.Duration},
		{"response_header_timeout", c.ResponseHeaderTimeout.Duration},
	} {
		if timeout.value <= 0 || timeout.value > time.Minute {
			return fmt.Errorf("collector.%s must be positive and at most 60s", timeout.name)
		}
	}
	if c.RetryInitialBackoff.Duration < time.Millisecond || c.RetryInitialBackoff.Duration > 10*time.Second {
		return errors.New("collector.retry_initial_backoff must be between 1ms and 10s")
	}
	if c.RetryMaxBackoff.Duration < c.RetryInitialBackoff.Duration || c.RetryMaxBackoff.Duration > time.Minute {
		return errors.New("collector.retry_max_backoff must be between retry_initial_backoff and 60s")
	}
	if cfg.Storage.HistoryRetention < storage.MinHistoryRetention || cfg.Storage.HistoryRetention > storage.MaxHistoryRetention {
		return errors.New("storage.history_retention must be between 1 and 1000")
	}
	if cfg.Runtime.ShutdownTimeout.Duration < time.Second || cfg.Runtime.ShutdownTimeout.Duration > time.Minute {
		return errors.New("runtime.shutdown_timeout must be between 1s and 60s")
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("logging.level must be debug, info, warn, or error")
	}
	switch cfg.Logging.Format {
	case "text", "json":
	default:
		return errors.New("logging.format must be text or json")
	}
	return nil
}

func resolvePaths(home string, cfg File) (Paths, error) {
	p := Paths{
		Home:        home,
		ConfigFile:  filepath.Join(home, "config.toml"),
		SourcesFile: filepath.Join(home, "sources.toml"),
	}
	targets := []struct {
		name string
		raw  string
		set  func(string)
	}{
		{"server.consumer_socket", cfg.Server.ConsumerSocket, func(v string) { p.ConsumerSocket = v }},
		{"server.admin_socket", cfg.Server.AdminSocket, func(v string) { p.AdminSocket = v }},
		{"storage.database", cfg.Storage.Database, func(v string) { p.Database = v }},
		{"storage.marker", cfg.Storage.Marker, func(v string) { p.Marker = v }},
		{"storage.lock", cfg.Storage.Lock, func(v string) { p.Lock = v }},
	}
	seen := map[string]string{
		p.ConfigFile:                "config.toml",
		p.SourcesFile:               "sources.toml",
		filepath.Join(home, "data"): "data directory",
		filepath.Join(home, "logs"): "logs directory",
		filepath.Join(home, "run"):  "run directory",
	}
	for _, target := range targets {
		resolved, err := safeRelativePath(home, target.raw)
		if err != nil {
			return Paths{}, fmt.Errorf("%s: %w", target.name, err)
		}
		if other, exists := seen[resolved]; exists {
			return Paths{}, fmt.Errorf("%s aliases %s", target.name, other)
		}
		seen[resolved] = target.name
		target.set(resolved)
	}
	canonicalLock := filepath.Join(home, filepath.FromSlash(HomeLockRelativePath))
	if p.Lock != canonicalLock {
		return Paths{}, fmt.Errorf("storage.lock must equal %q", HomeLockRelativePath)
	}
	if filepath.Dir(p.Database) != filepath.Dir(p.Marker) {
		return Paths{}, errors.New("storage.database and storage.marker must share a directory")
	}
	return p, nil
}

func validatePrivateDirectories(paths Paths) error {
	directories := map[string]struct{}{
		paths.Home:                         {},
		filepath.Join(paths.Home, "data"):  {},
		filepath.Join(paths.Home, "logs"):  {},
		filepath.Join(paths.Home, "run"):   {},
		filepath.Dir(paths.ConsumerSocket): {},
		filepath.Dir(paths.AdminSocket):    {},
		filepath.Dir(paths.Database):       {},
		filepath.Dir(paths.Marker):         {},
		filepath.Dir(paths.Lock):           {},
	}
	for directory := range directories {
		current := directory
		for {
			relative, err := filepath.Rel(paths.Home, current)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("directory %q escapes home", directory)
			}
			info, err := os.Lstat(current)
			if err != nil {
				return fmt.Errorf("inspect private directory %q: %w", current, err)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("%q is not a safe directory", current)
			}
			if info.Mode().Perm() != 0o700 {
				return fmt.Errorf("%q permissions must be 0700", current)
			}
			if current == paths.Home {
				break
			}
			parent := filepath.Dir(current)
			if parent == current {
				return fmt.Errorf("directory %q does not descend from home", directory)
			}
			current = parent
		}
	}
	return nil
}

func safeRelativePath(home, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("path must be a clean non-empty relative path")
	}
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes home")
	}
	resolved := filepath.Join(home, path)
	relative, err := filepath.Rel(home, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes home")
	}
	current := home
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path traverses symlink %q", current)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("path component %q is not a directory", current)
		}
	}
	return resolved, nil
}

func buildPlans(sources SourcesFile, policy domain.CollectorPolicy) ([]domain.FeedPlan, [32]byte, error) {
	feeds := make([]domain.FeedPlan, 0, len(sources.Feeds))
	for _, feed := range sources.Feeds {
		sources := make([]domain.SourcePlan, 0, len(feed.Sources))
		for _, source := range feed.Sources {
			sources = append(sources, domain.SourcePlan{
				ID:          source.ID,
				URL:         source.URL,
				JSONPointer: source.JSONPointer,
			})
		}
		feeds = append(feeds, domain.FeedPlan{
			Symbol:     feed.Symbol,
			Interval:   feed.Interval.Duration,
			StaleAfter: feed.StaleAfter.Duration,
			Sources:    sources,
		})
	}
	return domain.CanonicalPlans(feeds, policy)
}

func collectorPolicy(cfg Collector) domain.CollectorPolicy {
	return domain.CollectorPolicy{
		MaxConcurrency:        cfg.MaxConcurrency,
		SourceResponseBytes:   cfg.SourceResponseBytes,
		MaxRedirects:          cfg.MaxRedirects,
		MaxAttempts:           cfg.MaxAttempts,
		RequestTimeout:        cfg.RequestTimeout.Duration,
		ConnectTimeout:        cfg.ConnectTimeout.Duration,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout.Duration,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout.Duration,
		RetryInitialBackoff:   cfg.RetryInitialBackoff.Duration,
		RetryMaxBackoff:       cfg.RetryMaxBackoff.Duration,
	}
}

func readBoundedRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("path is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("file permissions are not private")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, errors.New("file changed while opening")
	}
	if openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("file permissions are not private")
	}
	if openedInfo.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return data, nil
}

// PrepareInitialHome creates and validates the managed private directories
// required before acquiring a home's ownership lock.
func PrepareInitialHome(home string) (string, error) {
	absoluteHome, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	if err := RejectSymlinkPath(filepath.Dir(absoluteHome)); err != nil {
		return "", err
	}
	if err := prepareInitialHomeDirectories(absoluteHome); err != nil {
		return "", err
	}
	return absoluteHome, nil
}

func prepareInitialHomeDirectories(absoluteHome string) error {
	for _, directory := range []string{absoluteHome, filepath.Join(absoluteHome, "data"), filepath.Join(absoluteHome, "logs"), filepath.Join(absoluteHome, "run")} {
		if err := ensurePrivateDir(directory); err != nil {
			return err
		}
	}
	return nil
}

func WriteInitialFiles(home string) (Paths, error) {
	absoluteHome, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, err
	}
	if err := RejectSymlinkPath(filepath.Dir(absoluteHome)); err != nil {
		return Paths{}, fmt.Errorf("validate home parent: %w", err)
	}
	if err := prepareInitialHomeDirectories(absoluteHome); err != nil {
		return Paths{}, err
	}
	revisionBytes := make([]byte, 16)
	if _, err := rand.Read(revisionBytes); err != nil {
		return Paths{}, fmt.Errorf("generate publication revision: %w", err)
	}
	revision := hex.EncodeToString(revisionBytes)
	sources := []byte("schema_version = 1\npublication_revision = \"" + revision + "\"\nfeeds = []\n")
	digest := sha256.Sum256(sources)
	config := []byte(initialConfigTemplate(revision, hex.EncodeToString(digest[:])))

	paths := Paths{
		Home:           absoluteHome,
		ConfigFile:     filepath.Join(absoluteHome, "config.toml"),
		SourcesFile:    filepath.Join(absoluteHome, "sources.toml"),
		ConsumerSocket: filepath.Join(absoluteHome, "run", "oracle.sock"),
		AdminSocket:    filepath.Join(absoluteHome, "run", "admin.sock"),
		Database:       filepath.Join(absoluteHome, "data", "oracle.db"),
		Marker:         filepath.Join(absoluteHome, "data", "storage.meta"),
		Lock:           filepath.Join(absoluteHome, "run", "oracled.lock"),
	}
	for _, path := range []string{paths.ConfigFile, paths.SourcesFile, paths.Database, paths.Marker, paths.ConsumerSocket, paths.AdminSocket} {
		if _, err := os.Lstat(path); err == nil {
			return Paths{}, fmt.Errorf("refusing to overwrite existing %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Paths{}, err
		}
	}
	if err := writeExclusive(paths.SourcesFile, sources); err != nil {
		return Paths{}, err
	}
	if err := writeExclusive(paths.ConfigFile, config); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

// RejectSymlinkPath rejects every existing component of an absolute path.
func RejectSymlinkPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	remainder := strings.TrimPrefix(absolute, volume)
	remainder = strings.TrimPrefix(remainder, string(filepath.Separator))
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(remainder, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path traverses symlink %q", current)
		}
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s is not a safe directory", path)
		}
		if info.Mode().Perm() != 0o700 {
			return fmt.Errorf("%s permissions must be 0700", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Mkdir(path, 0o700)
}

func writeExclusive(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func initialConfigTemplate(revision, digest string) string {
	return `schema_version = 1
publication_revision = "` + revision + `"
sources_sha256 = "` + digest + `"

[server]
consumer_socket = "run/oracle.sock"
admin_socket = "run/admin.sock"
max_request_bytes = 65536
max_response_bytes = 1048576

[collector]
max_concurrency = 32
source_response_bytes = 1048576
max_redirects = 3
max_attempts = 3
request_timeout = "5s"
connect_timeout = "2s"
tls_handshake_timeout = "2s"
response_header_timeout = "3s"
retry_initial_backoff = "100ms"
retry_max_backoff = "1s"

[storage]
database = "data/oracle.db"
marker = "data/storage.meta"
lock = "run/oracled.lock"
history_retention = 30

[runtime]
shutdown_timeout = "10s"

[logging]
level = "info"
format = "text"
`
}
