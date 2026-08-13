package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sys/unix"
)

func TestStoreInitializeActivateInsertHistoryAndReopen(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	if store.db.NoSync || store.db.NoGrowSync {
		t.Fatal("synchronous durability options were disabled")
	}
	feeds, digest := testPlans(t, "a")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ActivationGeneration != 1 {
		t.Fatalf("generation = %d", catalog.ActivationGeneration)
	}
	for i := 0; i < 3; i++ {
		record := testAggregate(feeds[0], catalog.ActivationGeneration, int64(i))
		committed, err := store.Insert(record, 2)
		if err != nil {
			t.Fatal(err)
		}
		if committed.Sequence != uint64(i+1) {
			t.Fatalf("sequence = %d", committed.Sequence)
		}
	}
	page, err := store.History("BTC/USD", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.HighWater != 3 || len(page.Records) != 1 || page.Records[0].Sequence != 3 || len(page.NextPageToken) == 0 {
		t.Fatalf("unexpected first page: %#v", page)
	}
	second, err := store.History("BTC/USD", 1, page.NextPageToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Records[0].Sequence != 2 || len(second.NextPageToken) != 0 {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, reopened)
	latest, err := reopened.LatestCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if latest["BTC/USD"].Sequence != 3 {
		t.Fatalf("latest = %#v", latest)
	}
}

func TestOpenDoesNotRecreateMissingDatabase(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(database, marker, false); err == nil {
		_ = store.Close()
		t.Fatal("Open recreated a missing database")
	}
	if _, err := os.Lstat(database); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing database was created or replaced: %v", err)
	}
}

func TestOpenReadOnlyAcceptsOwnerReadOnlyDatabase(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(database, 0o400); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, true)
	if err != nil {
		t.Fatal(err)
	}
	if !store.readOnly || !store.db.IsReadOnly() {
		t.Fatal("read-only Open returned a writable store")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExistingFileOpenerPinsLstatIdentity(t *testing.T) {
	t.Parallel()
	t.Run("opens existing file read-write", func(t *testing.T) {
		database, _, identity := initializedVerifiedPair(t)
		file, err := openExistingFile(database, os.O_RDWR|os.O_CREATE, identity)
		if err != nil {
			t.Fatal(err)
		}
		defer closeTestResource(t, file)
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		if flags&unix.O_ACCMODE != unix.O_RDWR {
			t.Fatalf("database access mode = %#x, want O_RDWR", flags&unix.O_ACCMODE)
		}
	})
	t.Run("opens existing file read-only", func(t *testing.T) {
		database, _, identity := initializedVerifiedPair(t)
		file, err := openExistingFile(database, os.O_RDONLY, identity)
		if err != nil {
			t.Fatal(err)
		}
		defer closeTestResource(t, file)
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		if flags&unix.O_ACCMODE != unix.O_RDONLY {
			t.Fatalf("database access mode = %#x, want O_RDONLY", flags&unix.O_ACCMODE)
		}
	})
	t.Run("rejects mutating flags before open", func(t *testing.T) {
		database, _, identity := initializedVerifiedPair(t)
		before, err := os.ReadFile(database)
		if err != nil {
			t.Fatal(err)
		}
		if file, err := openExistingFile(database, os.O_RDWR|os.O_TRUNC, identity); err == nil {
			_ = file.Close()
			t.Fatal("opener accepted O_TRUNC")
		}
		after, err := os.ReadFile(database)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("database was truncated while rejecting flags")
		}
	})
	t.Run("missing after lstat", func(t *testing.T) {
		database, _, identity := initializedVerifiedPair(t)
		if err := os.Remove(database); err != nil {
			t.Fatal(err)
		}
		if db, err := openVerifiedDatabase(database, identity, false); err == nil {
			_ = db.Close()
			t.Fatal("opener recreated a missing database")
		}
		if _, err := os.Lstat(database); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing database was created: %v", err)
		}
	})
	t.Run("regular-file replacement", func(t *testing.T) {
		database, _, identity := initializedVerifiedPair(t)
		original := database + ".original"
		if err := os.Rename(database, original); err != nil {
			t.Fatal(err)
		}
		replacement := []byte("replacement database sentinel")
		if err := os.WriteFile(database, replacement, 0o600); err != nil {
			t.Fatal(err)
		}
		if db, err := openVerifiedDatabase(database, identity, false); err == nil {
			_ = db.Close()
			t.Fatal("opener accepted a replacement database")
		}
		after, err := os.ReadFile(database)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, replacement) {
			t.Fatalf("replacement database was modified: %q", after)
		}
	})
	t.Run("symlink replacement", func(t *testing.T) {
		database, _, identity := initializedVerifiedPair(t)
		original := database + ".original"
		if err := os.Rename(database, original); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(original, database); err != nil {
			t.Fatal(err)
		}
		if db, err := openVerifiedDatabase(database, identity, false); err == nil {
			_ = db.Close()
			t.Fatal("opener followed a replacement symlink")
		}
		info, err := os.Lstat(database)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("database path is no longer the symlink: %v", info.Mode())
		}
		after, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("symlink target was modified")
		}
	})
	t.Run("marker rename and symlink to original inode", func(t *testing.T) {
		_, marker, _ := initializedVerifiedPair(t)
		identity, err := os.Lstat(marker)
		if err != nil {
			t.Fatal(err)
		}
		original := marker + ".original"
		if err := os.Rename(marker, original); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(original, marker); err != nil {
			t.Fatal(err)
		}
		if file, err := openExistingFile(marker, os.O_RDONLY, identity); err == nil {
			_ = file.Close()
			t.Fatal("opener followed marker symlink to the original inode")
		}
		after, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("marker symlink target was modified")
		}
	})
}

func TestActivationAtoBtoACannotResurrect(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	feedsA, digestA := testPlans(t, "a")
	first, err := store.Activate(digestA, feedsA, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Insert(testAggregate(feedsA[0], first.ActivationGeneration, 0), 30); err != nil {
		t.Fatal(err)
	}
	feedsB, digestB := testPlans(t, "b")
	second, err := store.Activate(digestB, feedsB, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.ActivationGeneration != 2 {
		t.Fatalf("second generation = %d", second.ActivationGeneration)
	}
	third, err := store.Activate(digestA, feedsA, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if third.ActivationGeneration != 3 {
		t.Fatalf("third generation = %d", third.ActivationGeneration)
	}
	latestRecords, err := store.LatestRecords()
	if err != nil {
		t.Fatal(err)
	}
	if latestRecords["BTC/USD"].ActivationGeneration != first.ActivationGeneration {
		t.Fatalf("latest record did not preserve prior-generation visibility: %#v", latestRecords)
	}
	latest, err := store.LatestCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 0 {
		t.Fatalf("prior A record resurrected: %#v", latest)
	}
	page, err := store.History("BTC/USD", 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ActivationGeneration != 1 {
		t.Fatalf("prior history was not retained: %#v", page.Records)
	}
}

func TestHistoryTokenExpiresWhenRetentionMoves(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	feeds, digest := testPlans(t, "a")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 3; i++ {
		if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, i), 2); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.History("BTC/USD", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, 3), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.History("BTC/USD", 1, page.NextPageToken); !errors.Is(err, ErrPageKeyExpired) {
		t.Fatalf("continuation error = %v", err)
	}
}

func TestHistoryRejectsMalformedRecordKey(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	feeds, digest := testPlans(t, "malformed-key")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, 0), 30); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketRecords).Bucket([]byte(feeds[0].Symbol))
		return bucket.Put([]byte{0x02}, []byte("malformed"))
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.History(feeds[0].Symbol, 30, nil); err == nil ||
		!strings.Contains(err.Error(), "record key length is invalid") {
		t.Fatalf("History error = %v", err)
	}
}

func TestHistoryRejectsMissingRecordsBucket(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	if err := store.db.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket(bucketRecords)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.History("BTC/USD", 30, nil); err == nil ||
		!strings.Contains(err.Error(), "records bucket is missing") {
		t.Fatalf("History error = %v", err)
	}
}

func TestHistoryRejectsOrphanRecordBucket(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	if err := store.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.Bucket(bucketRecords).CreateBucket([]byte("BTC/USD"))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.History("BTC/USD", 30, nil); err == nil ||
		!strings.Contains(err.Error(), "absent from current catalog") {
		t.Fatalf("History error = %v", err)
	}
}

func TestHistoryRejectsMissingBucketDuringPagination(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	feeds, digest := testPlans(t, "missing-page-bucket")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	for index := int64(0); index < 2; index++ {
		if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, index), 30); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.History(feeds[0].Symbol, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.NextPageToken) == 0 {
		t.Fatal("first page did not return a continuation token")
	}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRecords).DeleteBucket([]byte(feeds[0].Symbol))
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.History(feeds[0].Symbol, 1, page.NextPageToken); err == nil ||
		!strings.Contains(err.Error(), "configured record bucket is missing") {
		t.Fatalf("History continuation error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(database, marker, false); err == nil {
		_ = reopened.Close()
		t.Fatal("storage with a missing configured bucket reopened")
	}
}

func TestHistoryRejectsRecordKeyPayloadMismatch(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	feeds, digest := testPlans(t, "mismatched-record")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, 0), 30); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketRecords).Bucket([]byte(feeds[0].Symbol))
		var firstKey, secondKey [8]byte
		binary.BigEndian.PutUint64(firstKey[:], 1)
		binary.BigEndian.PutUint64(secondKey[:], 2)
		frame := append([]byte(nil), bucket.Get(firstKey[:])...)
		return bucket.Put(secondKey[:], frame)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.History(feeds[0].Symbol, 30, nil); err == nil ||
		!strings.Contains(err.Error(), "record key/payload mismatch") {
		t.Fatalf("History error = %v", err)
	}
}

func TestActivatePrunesReducedRetentionBeforeReadiness(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	feeds, digest := testPlans(t, "retention")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 5; i++ {
		if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, i), 30); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Activate(digest, feeds, 2, true); err != nil {
		t.Fatal(err)
	}
	page, err := store.History(feeds[0].Symbol, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || page.Records[0].Sequence != 5 || page.Records[1].Sequence != 4 {
		t.Fatalf("history after startup pruning = %#v", page.Records)
	}
}

func TestActivateCreatesEmptyConfiguredRecordBucket(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	feeds, digest := testPlans(t, "empty-bucket")
	if _, err := store.Activate(digest, feeds, 30, true); err != nil {
		t.Fatal(err)
	}
	page, err := store.History(feeds[0].Symbol, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("empty configured history = %#v", page.Records)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPageTokenAllowsMaximumHighWater(t *testing.T) {
	var secret [32]byte
	raw := encodePageToken(pageToken{
		Epoch:         1,
		Symbol:        "BTC/USD",
		HighWater:     ^uint64(0),
		InitialLow:    1,
		NextExclusive: ^uint64(0),
	}, secret)
	if _, err := decodePageToken(raw, secret); err != nil {
		t.Fatalf("decode maximum high-water token: %v", err)
	}
}

func TestHistoryContinuationAtMaximumSequenceDoesNotRepeat(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	store, err := Open(database, marker, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, store)
	feeds, digest := testPlans(t, "max")
	catalog, err := store.Activate(digest, feeds, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.Bucket(bucketRecords).CreateBucketIfNotExists([]byte(feeds[0].Symbol))
		if err != nil {
			return err
		}
		for _, sequence := range []uint64{^uint64(0) - 1, ^uint64(0)} {
			record := testAggregate(feeds[0], catalog.ActivationGeneration, 0)
			record.Sequence = sequence
			frame, err := encodeAggregate(record)
			if err != nil {
				return err
			}
			var key [8]byte
			binary.BigEndian.PutUint64(key[:], sequence)
			if err := bucket.Put(key[:], frame); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	first, err := store.History(feeds[0].Symbol, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 1 || first.Records[0].Sequence != ^uint64(0) || len(first.NextPageToken) == 0 {
		t.Fatalf("first page = %#v", first)
	}
	second, err := store.History(feeds[0].Symbol, 1, first.NextPageToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Records[0].Sequence != ^uint64(0)-1 ||
		len(second.NextPageToken) != 0 {
		t.Fatalf("second page = %#v", second)
	}
}

func closeTestResource(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close test resource: %v", err)
	}
}

func TestStoreRejectsPartialPairAndUnknownBucket(t *testing.T) {
	t.Parallel()
	t.Run("partial pair", func(t *testing.T) {
		directory := t.TempDir()
		database := filepath.Join(directory, "oracle.db")
		marker := filepath.Join(directory, "storage.meta")
		if err := os.WriteFile(marker, make([]byte, 60), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Initialize(database, marker); err == nil {
			t.Fatal("Initialize accepted a partial pair")
		}
	})
	t.Run("oversized marker", func(t *testing.T) {
		directory := t.TempDir()
		database := filepath.Join(directory, "oracle.db")
		marker := filepath.Join(directory, "storage.meta")
		if err := Initialize(database, marker); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, make([]byte, 1<<20), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(database, marker, false); err == nil ||
			!strings.Contains(err.Error(), "marker size") {
			t.Fatalf("Open oversized marker error = %v", err)
		}
	})
	t.Run("unknown bucket", func(t *testing.T) {
		directory := t.TempDir()
		database := filepath.Join(directory, "oracle.db")
		marker := filepath.Join(directory, "storage.meta")
		if err := Initialize(database, marker); err != nil {
			t.Fatal(err)
		}
		db, err := bolt.Open(database, 0o600, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucket([]byte("unknown"))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		if _, err := Open(database, marker, false); err == nil {
			t.Fatal("Open accepted unknown bucket")
		}
	})
}

func TestVerifyDatabaseRejectsRecordGenerationInvariantViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		want    string
		records func(feedA, feedB domain.FeedPlan) []domain.Aggregate
	}{
		{
			name: "generation above catalog",
			want: "record activation generation exceeds catalog",
			records: func(_, feedB domain.FeedPlan) []domain.Aggregate {
				record := testAggregate(feedB, 3, 0)
				record.Sequence = 1
				return []domain.Aggregate{record}
			},
		},
		{
			name: "generation regression",
			want: "record activation generation regresses",
			records: func(feedA, feedB domain.FeedPlan) []domain.Aggregate {
				first := testAggregate(feedB, 2, 0)
				first.Sequence = 1
				second := testAggregate(feedA, 1, 1)
				second.Sequence = 2
				return []domain.Aggregate{first, second}
			},
		},
		{
			name: "conflicting fingerprint",
			want: "record generation contract conflicts",
			records: func(feedA, feedB domain.FeedPlan) []domain.Aggregate {
				first := testAggregate(feedA, 1, 0)
				first.Sequence = 1
				second := testAggregate(feedB, 1, 1)
				second.Sequence = 2
				return []domain.Aggregate{first, second}
			},
		},
		{
			name: "conflicting configured source count",
			want: "record generation contract conflicts",
			records: func(feedA, _ domain.FeedPlan) []domain.Aggregate {
				first := testAggregate(feedA, 1, 0)
				first.Sequence = 1
				second := testAggregate(feedA, 1, 1)
				second.Sequence = 2
				second.ConfiguredSources = 4
				return []domain.Aggregate{first, second}
			},
		},
		{
			name: "current fingerprint mismatch",
			want: "current-generation record fingerprint mismatches catalog",
			records: func(feedA, _ domain.FeedPlan) []domain.Aggregate {
				record := testAggregate(feedA, 2, 0)
				record.Sequence = 1
				return []domain.Aggregate{record}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			database := filepath.Join(directory, "oracle.db")
			marker := filepath.Join(directory, "storage.meta")
			if err := Initialize(database, marker); err != nil {
				t.Fatal(err)
			}
			store, err := Open(database, marker, false)
			if err != nil {
				t.Fatal(err)
			}
			feedsA, digestA := testPlans(t, "scan-a")
			if _, err := store.Activate(digestA, feedsA, 30, false); err != nil {
				t.Fatal(err)
			}
			feedsB, digestB := testPlans(t, "scan-b")
			if _, err := store.Activate(digestB, feedsB, 30, false); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			db, err := bolt.Open(database, 0o600, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = db.Update(func(tx *bolt.Tx) error {
				bucket, err := tx.Bucket(bucketRecords).CreateBucketIfNotExists([]byte(feedsA[0].Symbol))
				if err != nil {
					return err
				}
				for _, record := range test.records(feedsA[0], feedsB[0]) {
					frame, err := encodeAggregate(record)
					if err != nil {
						return err
					}
					var key [8]byte
					binary.BigEndian.PutUint64(key[:], record.Sequence)
					if err := bucket.Put(key[:], frame); err != nil {
						return err
					}
				}
				return nil
			})
			closeErr := db.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			opened, err := Open(database, marker, false)
			if opened != nil {
				_ = opened.Close()
			}
			if err == nil {
				t.Fatal("Open accepted invalid record generation state")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Open error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestFrameRejectsCorruptionAndOversizedClaim(t *testing.T) {
	t.Parallel()
	feeds, _ := testPlans(t, "a")
	record := testAggregate(feeds[0], 1, 0)
	record.Sequence = 1
	frame, err := encodeAggregate(record)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), frame...)
	corrupt[12] ^= 1
	if _, err := decodeAggregate(corrupt); err == nil {
		t.Fatal("checksum corruption was accepted")
	}
	oversized := append([]byte(nil), frame[:10+sha256.Size]...)
	binary.BigEndian.PutUint32(oversized[6:10], maxAggregatePayload+1)
	if _, err := decodeAggregate(oversized); err == nil {
		t.Fatal("oversized payload claim was accepted")
	}
}

func TestAggregateFrameAllowsBackwardWallClockProvenance(t *testing.T) {
	t.Parallel()
	feeds, _ := testPlans(t, "wall-clock")
	record := testAggregate(feeds[0], 1, 0)
	record.Sequence = 1
	record.CycleStartedAt = record.CollectedAt.Add(time.Second)
	frame, err := encodeAggregate(record)
	if err != nil {
		t.Fatalf("encode wall-clock inversion: %v", err)
	}
	decoded, err := decodeAggregate(frame)
	if err != nil {
		t.Fatalf("decode wall-clock inversion: %v", err)
	}
	if !decoded.CycleStartedAt.After(decoded.CollectedAt) {
		t.Fatalf("wall-clock provenance was unexpectedly rewritten: %#v", decoded)
	}
}

func TestCloseAndJoinPreservesPrimaryAndCloseErrors(t *testing.T) {
	t.Parallel()
	primaryErr := errors.New("primary failure")
	closeErr := errors.New("close failure")
	closer := &recordingCloser{err: closeErr}

	err := closeAndJoin(primaryErr, closer)
	if !errors.Is(err, primaryErr) {
		t.Fatalf("joined error %v does not preserve primary error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("joined error %v does not preserve close error", err)
	}
	if closer.calls != 1 {
		t.Fatalf("Close calls = %d, want 1", closer.calls)
	}
}

type recordingCloser struct {
	err   error
	calls int
}

func (c *recordingCloser) Close() error {
	c.calls++
	return c.err
}

func initializedVerifiedPair(t *testing.T) (string, string, os.FileInfo) {
	t.Helper()
	directory := t.TempDir()
	database := filepath.Join(directory, "oracle.db")
	marker := filepath.Join(directory, "storage.meta")
	if err := Initialize(database, marker); err != nil {
		t.Fatal(err)
	}
	_, identity, err := verifyFinalFiles(database, marker)
	if err != nil {
		t.Fatal(err)
	}
	return database, marker, identity
}

func testPlans(t *testing.T, sourceSuffix string) ([]domain.FeedPlan, [32]byte) {
	t.Helper()
	feeds, digest, err := domain.CanonicalPlans([]domain.FeedPlan{{
		Symbol:     "BTC/USD",
		Interval:   time.Second,
		StaleAfter: 5 * time.Second,
		Sources: []domain.SourcePlan{
			{ID: "a", URL: "https://a.example/" + sourceSuffix, JSONPointer: "/v"},
			{ID: "b", URL: "https://b.example/" + sourceSuffix, JSONPointer: "/v"},
			{ID: "c", URL: "https://c.example/" + sourceSuffix, JSONPointer: "/v"},
		},
	}}, domain.CollectorPolicy{
		MaxConcurrency:        3,
		SourceResponseBytes:   1024,
		MaxRedirects:          1,
		MaxAttempts:           1,
		RequestTimeout:        time.Second,
		ConnectTimeout:        time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		RetryInitialBackoff:   time.Millisecond,
		RetryMaxBackoff:       time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return feeds, digest
}

func testAggregate(feed domain.FeedPlan, generation uint64, offset int64) domain.Aggregate {
	now := time.Unix(1_800_000_000+offset, 0).UTC()
	return domain.Aggregate{
		Symbol:               feed.Symbol,
		Value:                "10.000000000000000000",
		ActivationGeneration: generation,
		CycleStartedAt:       now,
		CollectedAt:          now.Add(time.Millisecond),
		ConfiguredSources:    3,
		SuccessfulSources:    3,
		ContributorIDs:       []string{"a", "b", "c"},
		FeedPlanFingerprint:  feed.Fingerprint,
	}
}
