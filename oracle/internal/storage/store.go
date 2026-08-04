package storage

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sys/unix"
)

const (
	MinHistoryRetention    = 1
	MaxHistoryRetention    = 1000
	MinHistoryPageSize     = 1
	MaxHistoryPageSize     = 50
	DefaultHistoryPageSize = 30
)

var (
	bucketMeta    = []byte("meta")
	bucketCatalog = []byte("catalog")
	bucketRecords = []byte("records")
	keyCatalog    = []byte("current")

	metaSchema  = []byte("schema_version")
	metaStoreID = []byte("store_id")
	metaPair    = []byte("pair_digest")
	metaEpoch   = []byte("page_key_epoch")
	metaSecret  = []byte("page_key_secret")

	markerMagic  = []byte("GORAMETA")
	markerDomain = []byte("guru-oracled/store-marker/v1\x00")
	pairDomain   = []byte("guru-oracled/store-pair/v1\x00")
)

var (
	ErrHistoryNotFound = errors.New("history_not_found")
	ErrInvalidPageKey  = errors.New("invalid_page_key")
	ErrPageKeyMismatch = errors.New("page_key_symbol_mismatch")
	ErrPageKeyExpired  = errors.New("page_key_expired")
)

type Store struct {
	db       *bolt.DB
	readOnly bool

	mu      sync.RWMutex
	catalog Catalog
	epoch   uint64
	secret  [32]byte
}

type HistoryPage struct {
	Symbol        string
	HighWater     uint64
	Records       []domain.Aggregate
	NextPageToken []byte
}

func Initialize(databasePath, markerPath string) error {
	if err := requireBothAbsent(databasePath, markerPath); err != nil {
		return err
	}
	storeID := make([]byte, 16)
	secret := make([]byte, 32)
	if _, err := rand.Read(storeID); err != nil {
		return err
	}
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	marker := buildMarker(storeID)
	pairDigest := sha256.Sum256(append(append([]byte{}, pairDomain...), marker...))

	databaseDir := filepath.Dir(databasePath)
	markerDir := filepath.Dir(markerPath)
	if databaseDir != markerDir {
		return errors.New("database and marker must share a directory")
	}
	dbTemp, err := os.CreateTemp(databaseDir, ".oracle-db-*")
	if err != nil {
		return err
	}
	dbTempPath := dbTemp.Name()
	if err := dbTemp.Close(); err != nil {
		return err
	}
	if err := os.Remove(dbTempPath); err != nil {
		return err
	}
	cleanupDB := true
	defer func() {
		if cleanupDB {
			_ = os.Remove(dbTempPath)
		}
	}()

	db, err := bolt.Open(dbTempPath, 0o600, &bolt.Options{NoSync: false, NoGrowSync: false, NoFreelistSync: false})
	if err != nil {
		return err
	}
	emptyDigest := sha256.Sum256(nil)
	emptyFrame, err := encodeCatalog(Catalog{ActivationGeneration: 0, PlanDigest: emptyDigest, Feeds: []CatalogFeed{}})
	if err == nil {
		err = db.Update(func(tx *bolt.Tx) error {
			meta, err := tx.CreateBucket(bucketMeta)
			if err != nil {
				return err
			}
			catalog, err := tx.CreateBucket(bucketCatalog)
			if err != nil {
				return err
			}
			if _, err := tx.CreateBucket(bucketRecords); err != nil {
				return err
			}
			var schema [4]byte
			binary.BigEndian.PutUint32(schema[:], storageSchemaVersion)
			var epoch [8]byte
			return errors.Join(
				meta.Put(metaSchema, schema[:]),
				meta.Put(metaStoreID, storeID),
				meta.Put(metaPair, pairDigest[:]),
				meta.Put(metaEpoch, epoch[:]),
				meta.Put(metaSecret, secret),
				catalog.Put(keyCatalog, emptyFrame),
			)
		})
	}
	if err == nil {
		err = db.Sync()
	}
	if err := closeAndJoin(err, db); err != nil {
		return err
	}

	markerTemp, err := os.CreateTemp(markerDir, ".oracle-marker-*")
	if err != nil {
		return err
	}
	markerTempPath := markerTemp.Name()
	cleanupMarker := true
	defer func() {
		if cleanupMarker {
			_ = os.Remove(markerTempPath)
		}
	}()
	if err := markerTemp.Chmod(0o600); err != nil {
		_ = markerTemp.Close()
		return err
	}
	if _, err := markerTemp.Write(marker); err != nil {
		_ = markerTemp.Close()
		return err
	}
	if err := markerTemp.Sync(); err != nil {
		_ = markerTemp.Close()
		return err
	}
	if err := markerTemp.Close(); err != nil {
		return err
	}
	if err := os.Rename(dbTempPath, databasePath); err != nil {
		return err
	}
	cleanupDB = false
	if err := syncDirectory(databaseDir); err != nil {
		return err
	}
	if err := os.Rename(markerTempPath, markerPath); err != nil {
		return err
	}
	cleanupMarker = false
	return syncDirectory(markerDir)
}

func Open(databasePath, markerPath string, readOnly bool) (*Store, error) {
	marker, databaseIdentity, err := verifyFinalFiles(databasePath, markerPath)
	if err != nil {
		return nil, err
	}
	db, err := openVerifiedDatabase(databasePath, databaseIdentity, readOnly)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, readOnly: readOnly}
	catalog, epoch, secret, err := verifyDatabase(db, marker)
	if err != nil {
		return nil, closeAndJoin(err, db)
	}
	store.catalog = catalog
	store.epoch = epoch
	store.secret = secret
	return store, nil
}

func openVerifiedDatabase(databasePath string, databaseIdentity os.FileInfo, readOnly bool) (*bolt.DB, error) {
	options := &bolt.Options{
		ReadOnly:       readOnly,
		NoSync:         false,
		NoGrowSync:     false,
		NoFreelistSync: false,
		OpenFile: func(path string, flags int, _ os.FileMode) (*os.File, error) {
			if path != databasePath {
				return nil, errors.New("bbolt requested an unexpected database path")
			}
			return openExistingFile(path, flags, databaseIdentity)
		},
	}
	return bolt.Open(databasePath, 0o600, options)
}

func openExistingFile(path string, requestedFlags int, expected os.FileInfo) (*os.File, error) {
	if expected == nil {
		return nil, errors.New("file identity is missing")
	}
	accessMode := requestedFlags & unix.O_ACCMODE
	if accessMode != os.O_RDONLY && accessMode != os.O_RDWR {
		return nil, errors.New("existing file access mode is invalid")
	}
	if requestedFlags&^os.O_CREATE != accessMode {
		return nil, errors.New("existing file flags contain a mutating option")
	}
	file, err := os.OpenFile(path, accessMode|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open existing file: %w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("stat opened file: %w", err), file.Close())
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(expected, openedInfo) {
		return nil, errors.Join(errors.New("file changed while opening"), file.Close())
	}
	if openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.Join(errors.New("file permissions are not private"), file.Close())
	}
	return file, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) Activate(
	planDigest [32]byte,
	feeds []domain.FeedPlan,
	retention uint32,
	rotatePageKey bool,
) (Catalog, error) {
	if s.readOnly {
		return Catalog{}, errors.New("storage is read only")
	}
	if retention < MinHistoryRetention || retention > MaxHistoryRetention {
		return Catalog{}, errors.New("retention is out of range")
	}
	expectedFeeds := make([]CatalogFeed, len(feeds))
	for i, feed := range feeds {
		expectedFeeds[i] = CatalogFeed{Symbol: feed.Symbol, Fingerprint: feed.Fingerprint}
	}
	expectedFeeds = canonicalCatalog(expectedFeeds)
	var newSecret [32]byte
	if rotatePageKey {
		if _, err := rand.Read(newSecret[:]); err != nil {
			return Catalog{}, err
		}
	}

	var activated Catalog
	var epoch uint64
	err := s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		catalogBucket := tx.Bucket(bucketCatalog)
		records := tx.Bucket(bucketRecords)
		if meta == nil || catalogBucket == nil || records == nil {
			return errors.New("required storage bucket is missing")
		}
		current, err := decodeCatalog(catalogBucket.Get(keyCatalog))
		if err != nil {
			return err
		}
		activated = current
		if current.PlanDigest != planDigest {
			if current.ActivationGeneration == ^uint64(0) {
				return errors.New("activation generation overflow")
			}
			activated = Catalog{
				ActivationGeneration: current.ActivationGeneration + 1,
				PlanDigest:           planDigest,
				Feeds:                expectedFeeds,
			}
			keep := make(map[string]struct{}, len(expectedFeeds))
			for _, feed := range expectedFeeds {
				keep[feed.Symbol] = struct{}{}
			}
			var remove [][]byte
			if err := records.ForEach(func(name, value []byte) error {
				if value != nil {
					return errors.New("records contains a value")
				}
				if _, ok := keep[string(name)]; !ok {
					remove = append(remove, append([]byte(nil), name...))
				}
				return nil
			}); err != nil {
				return err
			}
			for _, name := range remove {
				if err := records.DeleteBucket(name); err != nil {
					return err
				}
			}
			frame, err := encodeCatalog(activated)
			if err != nil {
				return err
			}
			if err := catalogBucket.Put(keyCatalog, frame); err != nil {
				return err
			}
		} else if !catalogFeedsEqual(current.Feeds, expectedFeeds) {
			return errors.New("catalog plan digest collision")
		}
		for _, feed := range expectedFeeds {
			bucket, err := records.CreateBucketIfNotExists([]byte(feed.Symbol))
			if err != nil {
				return err
			}
			recordCount := bucket.Stats().KeyN
			for recordCount > int(retention) {
				first, _ := bucket.Cursor().First()
				if first == nil {
					return errors.New("activation retention cursor unexpectedly empty")
				}
				if err := bucket.Delete(first); err != nil {
					return err
				}
				recordCount--
			}
		}
		epoch = binary.BigEndian.Uint64(meta.Get(metaEpoch))
		if rotatePageKey {
			if epoch == ^uint64(0) {
				return errors.New("page-key epoch overflow")
			}
			epoch++
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], epoch)
			if err := meta.Put(metaEpoch, encoded[:]); err != nil {
				return err
			}
			if err := meta.Put(metaSecret, newSecret[:]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Catalog{}, err
	}
	s.mu.Lock()
	s.catalog = activated
	s.epoch = epoch
	if rotatePageKey {
		s.secret = newSecret
	}
	s.mu.Unlock()
	return activated, nil
}

func (s *Store) Catalog() Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := s.catalog
	result.Feeds = append([]CatalogFeed(nil), result.Feeds...)
	return result
}

func (s *Store) LatestCurrent() (map[string]domain.Aggregate, error) {
	latest, err := s.LatestRecords()
	if err != nil {
		return nil, err
	}
	catalog := s.Catalog()
	expected := make(map[string][32]byte, len(catalog.Feeds))
	for _, feed := range catalog.Feeds {
		expected[feed.Symbol] = feed.Fingerprint
	}
	result := make(map[string]domain.Aggregate, len(expected))
	for symbol, record := range latest {
		if record.ActivationGeneration == catalog.ActivationGeneration &&
			record.FeedPlanFingerprint == expected[symbol] {
			result[symbol] = record
		}
	}
	return result, nil
}

func (s *Store) LatestRecords() (map[string]domain.Aggregate, error) {
	catalog := s.Catalog()
	result := make(map[string]domain.Aggregate, len(catalog.Feeds))
	err := s.db.View(func(tx *bolt.Tx) error {
		records := tx.Bucket(bucketRecords)
		if records == nil {
			return errors.New("records bucket is missing")
		}
		for _, feed := range catalog.Feeds {
			bucket := records.Bucket([]byte(feed.Symbol))
			if bucket == nil {
				return errors.New("configured record bucket is missing")
			}
			_, raw := bucket.Cursor().Last()
			if raw == nil {
				continue
			}
			record, err := decodeAggregate(raw)
			if err != nil {
				return err
			}
			result[feed.Symbol] = record
		}
		return nil
	})
	return result, err
}

func (s *Store) Insert(candidate domain.Aggregate, retention uint32) (domain.Aggregate, error) {
	if s.readOnly {
		return domain.Aggregate{}, errors.New("storage is read only")
	}
	catalog := s.Catalog()
	var expected [32]byte
	found := false
	for _, feed := range catalog.Feeds {
		if feed.Symbol == candidate.Symbol {
			expected = feed.Fingerprint
			found = true
			break
		}
	}
	if !found || candidate.ActivationGeneration != catalog.ActivationGeneration ||
		candidate.FeedPlanFingerprint != expected {
		return domain.Aggregate{}, errors.New("aggregate does not match active catalog")
	}
	if retention < MinHistoryRetention || retention > MaxHistoryRetention {
		return domain.Aggregate{}, errors.New("retention is out of range")
	}
	var committed domain.Aggregate
	err := s.db.Update(func(tx *bolt.Tx) error {
		records := tx.Bucket(bucketRecords)
		if records == nil {
			return errors.New("records bucket is missing")
		}
		bucket := records.Bucket([]byte(candidate.Symbol))
		if bucket == nil {
			return errors.New("configured record bucket is missing")
		}
		lastKey, _ := bucket.Cursor().Last()
		var sequence uint64
		if lastKey != nil {
			if len(lastKey) != 8 {
				return errors.New("record key length is invalid")
			}
			sequence = binary.BigEndian.Uint64(lastKey)
		}
		if sequence == ^uint64(0) {
			return errors.New("aggregate sequence overflow")
		}
		candidate.Sequence = sequence + 1
		existingCount := bucket.Stats().KeyN
		frame, err := encodeAggregate(candidate)
		if err != nil {
			return err
		}
		var key [8]byte
		binary.BigEndian.PutUint64(key[:], candidate.Sequence)
		if err := bucket.Put(key[:], frame); err != nil {
			return err
		}
		recordCount := existingCount + 1
		for recordCount > int(retention) {
			first, _ := bucket.Cursor().First()
			if first == nil {
				return errors.New("retention cursor unexpectedly empty")
			}
			if err := bucket.Delete(first); err != nil {
				return err
			}
			recordCount--
		}
		committed = candidate
		return nil
	})
	return committed, err
}

func (s *Store) History(symbol string, pageSize uint32, token []byte) (HistoryPage, error) {
	if pageSize < MinHistoryPageSize || pageSize > MaxHistoryPageSize {
		return HistoryPage{}, errors.New("invalid page size")
	}
	normalized, err := domain.NormalizeSymbol(symbol)
	if err != nil || normalized != symbol {
		return HistoryPage{}, errors.New("invalid symbol")
	}
	s.mu.RLock()
	epoch, secret, catalog := s.epoch, s.secret, s.catalog
	s.mu.RUnlock()
	configured := false
	var currentFingerprint [32]byte
	for _, feed := range catalog.Feeds {
		if feed.Symbol == symbol {
			configured = true
			currentFingerprint = feed.Fingerprint
			break
		}
	}
	var parsed pageToken
	if len(token) > 0 {
		parsed, err = decodePageToken(token, secret)
		if err != nil {
			return HistoryPage{}, err
		}
		if parsed.Symbol != symbol {
			return HistoryPage{}, ErrPageKeyMismatch
		}
		if parsed.Epoch != epoch {
			return HistoryPage{}, ErrPageKeyExpired
		}
	}

	page := HistoryPage{Symbol: symbol, Records: []domain.Aggregate{}}
	err = s.db.View(func(tx *bolt.Tx) error {
		records := tx.Bucket(bucketRecords)
		if records == nil {
			return errors.New("records bucket is missing")
		}
		bucket := records.Bucket([]byte(symbol))
		if !configured {
			if bucket != nil {
				return errors.New("record bucket is absent from current catalog")
			}
			return ErrHistoryNotFound
		}
		if bucket == nil {
			return errors.New("configured record bucket is missing")
		}
		cursor := bucket.Cursor()
		lowKey, _ := cursor.First()
		highKey, _ := cursor.Last()
		if lowKey == nil || highKey == nil {
			if !configured {
				return ErrHistoryNotFound
			}
			return nil
		}
		if len(lowKey) != 8 || len(highKey) != 8 {
			return errors.New("record key length is invalid")
		}
		low := binary.BigEndian.Uint64(lowKey)
		high := binary.BigEndian.Uint64(highKey)
		if len(token) > 0 {
			if parsed.InitialLow != low {
				return ErrPageKeyExpired
			}
			high = parsed.HighWater
		}
		page.HighWater = high

		var key, raw []byte
		if len(token) == 0 {
			key, raw = cursor.Last()
		} else {
			var seek [8]byte
			binary.BigEndian.PutUint64(seek[:], parsed.NextExclusive)
			key, raw = cursor.Seek(seek[:])
			if key == nil {
				key, raw = cursor.Last()
			} else {
				if len(key) != 8 {
					return errors.New("record key length is invalid")
				}
				if binary.BigEndian.Uint64(key) >= parsed.NextExclusive {
					key, raw = cursor.Prev()
				}
			}
		}
		for key != nil && uint32(len(page.Records)) < pageSize {
			if len(key) != 8 {
				return errors.New("record key length is invalid")
			}
			sequence := binary.BigEndian.Uint64(key)
			if sequence <= high {
				record, err := decodeAggregate(raw)
				if err != nil {
					return err
				}
				if record.Symbol != symbol || record.Sequence != sequence {
					return errors.New("record key/payload mismatch")
				}
				if record.ActivationGeneration > catalog.ActivationGeneration {
					return errors.New("record activation generation exceeds catalog")
				}
				if record.ActivationGeneration == catalog.ActivationGeneration &&
					record.FeedPlanFingerprint != currentFingerprint {
					return errors.New("current-generation record fingerprint mismatch")
				}
				page.Records = append(page.Records, record)
			}
			key, raw = cursor.Prev()
		}
		if key != nil {
			if len(page.Records) == 0 {
				return errors.New("history continuation made no progress")
			}
			token := pageToken{
				Epoch:         epoch,
				Symbol:        symbol,
				HighWater:     high,
				InitialLow:    low,
				NextExclusive: page.Records[len(page.Records)-1].Sequence,
			}
			page.NextPageToken = encodePageToken(token, secret)
		}
		return nil
	})
	return page, err
}

func (s *Store) Sync() error {
	if s == nil || s.db == nil || s.readOnly {
		return nil
	}
	return s.db.Sync()
}

func requireBothAbsent(databasePath, markerPath string) error {
	dbExists, err := pathExists(databasePath)
	if err != nil {
		return err
	}
	markerExists, err := pathExists(markerPath)
	if err != nil {
		return err
	}
	if dbExists || markerExists {
		if dbExists != markerExists {
			return errors.New("storage database/marker pair is partial")
		}
		return errors.New("storage already exists")
	}
	return nil
}

func verifyFinalFiles(databasePath, markerPath string) ([]byte, os.FileInfo, error) {
	var databaseInfo, markerInfo os.FileInfo
	for _, path := range []string{databasePath, markerPath} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, nil, fmt.Errorf("storage pair missing: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("%s is not a regular file", path)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, nil, fmt.Errorf("%s permissions are not private", path)
		}
		if path == databasePath {
			databaseInfo = info
		}
		if path == markerPath {
			markerInfo = info
		}
	}
	if markerInfo == nil || markerInfo.Size() != 60 {
		return nil, nil, errors.New("storage marker size is invalid")
	}
	file, err := openExistingFile(markerPath, os.O_RDONLY, markerInfo)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(markerInfo, openedInfo) || openedInfo.Size() != 60 {
		return nil, nil, errors.New("storage marker changed while opening")
	}
	marker := make([]byte, 60)
	if _, err := io.ReadFull(file, marker); err != nil {
		return nil, nil, err
	}
	var extra [1]byte
	if count, readErr := file.Read(extra[:]); count != 0 || !errors.Is(readErr, io.EOF) {
		return nil, nil, errors.New("storage marker has trailing bytes")
	}
	if err := validateMarker(marker); err != nil {
		return nil, nil, err
	}
	return marker, databaseInfo, nil
}

func verifyDatabase(db *bolt.DB, marker []byte) (Catalog, uint64, [32]byte, error) {
	var catalog Catalog
	var epoch uint64
	var secret [32]byte
	err := db.View(func(tx *bolt.Tx) error {
		var structuralErr error
		for checkErr := range tx.Check() {
			if checkErr != nil && structuralErr == nil {
				structuralErr = fmt.Errorf("bbolt structural check: %w", checkErr)
			}
		}
		if structuralErr != nil {
			return structuralErr
		}
		allowedBuckets := map[string]struct{}{"meta": {}, "catalog": {}, "records": {}}
		if err := tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
			if _, ok := allowedBuckets[string(name)]; !ok {
				return fmt.Errorf("unknown top-level bucket %q", name)
			}
			return nil
		}); err != nil {
			return err
		}
		meta := tx.Bucket(bucketMeta)
		catalogBucket := tx.Bucket(bucketCatalog)
		records := tx.Bucket(bucketRecords)
		if meta == nil || catalogBucket == nil || records == nil {
			return errors.New("required storage bucket is missing")
		}
		allowedMeta := map[string]int{
			"schema_version":  4,
			"store_id":        16,
			"pair_digest":     32,
			"page_key_epoch":  8,
			"page_key_secret": 32,
		}
		count := 0
		if err := meta.ForEach(func(key, value []byte) error {
			length, ok := allowedMeta[string(key)]
			if !ok || value == nil || len(value) != length {
				return fmt.Errorf("invalid meta key %q", key)
			}
			count++
			return nil
		}); err != nil {
			return err
		}
		if count != len(allowedMeta) {
			return errors.New("storage metadata is incomplete")
		}
		if binary.BigEndian.Uint32(meta.Get(metaSchema)) != storageSchemaVersion {
			return errors.New("unsupported storage schema")
		}
		if !bytes.Equal(meta.Get(metaStoreID), marker[12:28]) {
			return errors.New("storage marker store ID mismatch")
		}
		pair := sha256.Sum256(append(append([]byte{}, pairDomain...), marker...))
		if !bytes.Equal(meta.Get(metaPair), pair[:]) {
			return errors.New("storage pair digest mismatch")
		}
		epoch = binary.BigEndian.Uint64(meta.Get(metaEpoch))
		copy(secret[:], meta.Get(metaSecret))

		catalogCount := 0
		if err := catalogBucket.ForEach(func(key, value []byte) error {
			if !bytes.Equal(key, keyCatalog) || value == nil {
				return errors.New("unknown catalog entry")
			}
			catalogCount++
			var err error
			catalog, err = decodeCatalog(value)
			return err
		}); err != nil {
			return err
		}
		if catalogCount != 1 {
			return errors.New("catalog/current is missing")
		}
		catalogSymbols := make(map[string][32]byte, len(catalog.Feeds))
		for _, feed := range catalog.Feeds {
			catalogSymbols[feed.Symbol] = feed.Fingerprint
		}
		seenSymbols := make(map[string]struct{}, len(catalogSymbols))
		if err := records.ForEach(func(name, value []byte) error {
			if value != nil {
				return errors.New("records contains a direct value")
			}
			symbol := string(name)
			normalized, err := domain.NormalizeSymbol(symbol)
			if err != nil || normalized != symbol {
				return errors.New("record bucket symbol is invalid")
			}
			currentFingerprint, exists := catalogSymbols[symbol]
			if !exists {
				return errors.New("record bucket is absent from current catalog")
			}
			seenSymbols[symbol] = struct{}{}
			bucket := records.Bucket(name)
			count := 0
			var previousSequence, previousGeneration uint64
			type generationContract struct {
				fingerprint      [32]byte
				configuredSource uint32
			}
			contracts := make(map[uint64]generationContract)
			return bucket.ForEach(func(key, raw []byte) error {
				if raw == nil || len(key) != 8 {
					return errors.New("record entry is invalid")
				}
				sequence := binary.BigEndian.Uint64(key)
				if sequence == 0 {
					return errors.New("record sequence key is zero")
				}
				if previousSequence != 0 && sequence != previousSequence+1 {
					return errors.New("record sequences are not contiguous")
				}
				previousSequence = sequence
				record, err := decodeAggregate(raw)
				if err != nil {
					return err
				}
				if record.Symbol != symbol || record.Sequence != sequence {
					return errors.New("record key/payload mismatch")
				}
				if record.ActivationGeneration > catalog.ActivationGeneration {
					return errors.New("record activation generation exceeds catalog")
				}
				if previousGeneration != 0 && record.ActivationGeneration < previousGeneration {
					return errors.New("record activation generation regresses")
				}
				previousGeneration = record.ActivationGeneration
				contract := generationContract{
					fingerprint:      record.FeedPlanFingerprint,
					configuredSource: record.ConfiguredSources,
				}
				if existing, ok := contracts[record.ActivationGeneration]; ok && existing != contract {
					return errors.New("record generation contract conflicts")
				}
				contracts[record.ActivationGeneration] = contract
				if record.ActivationGeneration == catalog.ActivationGeneration &&
					record.FeedPlanFingerprint != currentFingerprint {
					return errors.New("current-generation record fingerprint mismatches catalog")
				}
				count++
				if count > MaxHistoryRetention {
					return errors.New("record bucket exceeds maximum retention")
				}
				return nil
			})
		}); err != nil {
			return err
		}
		if len(seenSymbols) != len(catalogSymbols) {
			return errors.New("configured record bucket is missing")
		}
		return nil
	})
	return catalog, epoch, secret, err
}

func buildMarker(storeID []byte) []byte {
	marker := make([]byte, 60)
	copy(marker[:8], markerMagic)
	binary.BigEndian.PutUint32(marker[8:12], storageSchemaVersion)
	copy(marker[12:28], storeID)
	sum := sha256.Sum256(append(append([]byte{}, markerDomain...), marker[:28]...))
	copy(marker[28:], sum[:])
	return marker
}

func validateMarker(marker []byte) error {
	if len(marker) != 60 || !bytes.Equal(marker[:8], markerMagic) ||
		binary.BigEndian.Uint32(marker[8:12]) != storageSchemaVersion {
		return errors.New("storage marker header is invalid")
	}
	sum := sha256.Sum256(append(append([]byte{}, markerDomain...), marker[:28]...))
	if !bytes.Equal(sum[:], marker[28:]) {
		return errors.New("storage marker checksum mismatch")
	}
	return nil
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func closeAndJoin(primary error, closer io.Closer) error {
	return errors.Join(primary, closer.Close())
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func catalogFeedsEqual(left, right []CatalogFeed) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

type pageToken struct {
	Epoch         uint64
	Symbol        string
	HighWater     uint64
	InitialLow    uint64
	NextExclusive uint64
}

func encodePageToken(token pageToken, secret [32]byte) []byte {
	size := 4 + 1 + 8 + 2 + len(token.Symbol) + 8 + 8 + 8
	raw := make([]byte, size+sha256.Size)
	copy(raw[:4], []byte("ORPH"))
	raw[4] = 1
	binary.BigEndian.PutUint64(raw[5:13], token.Epoch)
	binary.BigEndian.PutUint16(raw[13:15], uint16(len(token.Symbol)))
	offset := 15
	copy(raw[offset:], token.Symbol)
	offset += len(token.Symbol)
	binary.BigEndian.PutUint64(raw[offset:offset+8], token.HighWater)
	offset += 8
	binary.BigEndian.PutUint64(raw[offset:offset+8], token.InitialLow)
	offset += 8
	binary.BigEndian.PutUint64(raw[offset:offset+8], token.NextExclusive)
	mac := hmac.New(sha256.New, secret[:])
	_, _ = mac.Write(raw[:size])
	copy(raw[size:], mac.Sum(nil))
	return raw
}

func decodePageToken(raw []byte, secret [32]byte) (pageToken, error) {
	if len(raw) < 4+1+8+2+8+8+8+sha256.Size || !bytes.Equal(raw[:4], []byte("ORPH")) || raw[4] != 1 {
		return pageToken{}, ErrInvalidPageKey
	}
	symbolLength := int(binary.BigEndian.Uint16(raw[13:15]))
	expected := 4 + 1 + 8 + 2 + symbolLength + 8 + 8 + 8 + sha256.Size
	if len(raw) != expected || symbolLength < 1 || symbolLength > domain.MaxSymbolBytes {
		return pageToken{}, ErrInvalidPageKey
	}
	bodyLength := len(raw) - sha256.Size
	mac := hmac.New(sha256.New, secret[:])
	_, _ = mac.Write(raw[:bodyLength])
	if !hmac.Equal(raw[bodyLength:], mac.Sum(nil)) {
		return pageToken{}, ErrInvalidPageKey
	}
	offset := 15
	token := pageToken{
		Epoch:  binary.BigEndian.Uint64(raw[5:13]),
		Symbol: string(raw[offset : offset+symbolLength]),
	}
	offset += symbolLength
	token.HighWater = binary.BigEndian.Uint64(raw[offset : offset+8])
	offset += 8
	token.InitialLow = binary.BigEndian.Uint64(raw[offset : offset+8])
	offset += 8
	token.NextExclusive = binary.BigEndian.Uint64(raw[offset : offset+8])
	if token.HighWater == 0 || token.InitialLow == 0 || token.NextExclusive == 0 ||
		token.InitialLow >= token.NextExclusive || token.NextExclusive > token.HighWater {
		return pageToken{}, ErrInvalidPageKey
	}
	return token, nil
}

func EncodePageKey(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodePageKey(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, ErrInvalidPageKey
	}
	if len(raw) > 512 {
		return nil, ErrInvalidPageKey
	}
	return raw, nil
}
