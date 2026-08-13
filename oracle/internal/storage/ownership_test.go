package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestAggregateSourceContractBoundaries(t *testing.T) {
	t.Parallel()
	t.Run("source counts", func(t *testing.T) {
		for _, test := range []struct {
			configured uint32
			successful uint32
			wantErr    bool
		}{
			{configured: 2, successful: 2, wantErr: true},
			{configured: 3, successful: 2},
			{configured: 64, successful: 33},
			{configured: 65, successful: 33, wantErr: true},
		} {
			t.Run(fmt.Sprintf("%d", test.configured), func(t *testing.T) {
				record := ownershipAggregate(test.configured, test.successful, ownershipContributorIDs(int(test.successful)))
				err := validateAggregate(record)
				if (err != nil) != test.wantErr {
					t.Fatalf("validateAggregate configured=%d successful=%d error = %v, wantErr %t", test.configured, test.successful, err, test.wantErr)
				}
				if test.wantErr && err.Error() != "aggregate source counts are invalid" {
					t.Fatalf("validateAggregate configured=%d error = %v", test.configured, err)
				}
			})
		}
	})

	t.Run("contributor id", func(t *testing.T) {
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
				record := ownershipAggregate(3, 2, []string{test.value, "z"})
				err := validateAggregate(record)
				if (err != nil) != test.wantErr {
					t.Fatalf("validateAggregate contributor %q error = %v, wantErr %t", test.value, err, test.wantErr)
				}
				if test.wantErr && err.Error() != "aggregate contributor id is invalid" {
					t.Fatalf("validateAggregate contributor %q error = %v", test.value, err)
				}
			})
		}
	})

	record := ownershipAggregate(4, 2, ownershipContributorIDs(2))
	if err := validateAggregate(record); err == nil || err.Error() != "aggregate is below strict-majority quorum" {
		t.Fatalf("validateAggregate below strict majority error = %v", err)
	}

	for _, ids := range [][]string{{"a", "a"}, {"b", "a"}} {
		record := ownershipAggregate(3, 2, ids)
		if err := validateAggregate(record); err == nil || err.Error() != "aggregate contributors are not sorted and unique" {
			t.Fatalf("validateAggregate contributors %v error = %v", ids, err)
		}
	}
}

func TestCatalogFeedCountBoundaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		feeds []CatalogFeed
	}{
		{name: "noncanonical", feeds: []CatalogFeed{{Symbol: "btc/usd"}}},
		{name: "unsorted", feeds: []CatalogFeed{{Symbol: "BTC/USD"}, {Symbol: "ADA/USD"}}},
		{name: "duplicate", feeds: []CatalogFeed{{Symbol: "BTC/USD"}, {Symbol: "BTC/USD"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateCatalog(Catalog{ActivationGeneration: 1, Feeds: test.feeds})
			want := "catalog feeds are not sorted and unique"
			if test.name == "noncanonical" {
				want = "catalog symbol is not canonical"
			}
			if err == nil || err.Error() != want {
				t.Fatalf("validateCatalog %s error = %v, want %q", test.name, err, want)
			}
		})
	}
	for _, test := range []struct {
		count   int
		wantErr bool
	}{
		{count: 256},
		{count: 257, wantErr: true},
	} {
		t.Run(fmt.Sprintf("%d", test.count), func(t *testing.T) {
			catalog := Catalog{ActivationGeneration: 1, Feeds: make([]CatalogFeed, test.count)}
			for i := range catalog.Feeds {
				catalog.Feeds[i].Symbol = fmt.Sprintf("ASSET/%03d", i)
			}
			err := validateCatalog(catalog)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateCatalog count %d error = %v, wantErr %t", test.count, err, test.wantErr)
			}
			if test.wantErr && err.Error() != "catalog has too many feeds" {
				t.Fatalf("validateCatalog count %d error = %v", test.count, err)
			}
		})
	}
}

func TestStorageRetentionBoundaries(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"activate", "insert"} {
		t.Run(operation, func(t *testing.T) {
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
					store := newOwnershipStore(t)
					feeds, digest := testPlans(t, fmt.Sprintf("%s-%d", operation, test.value))
					var err error
					switch operation {
					case "activate":
						_, err = store.Activate(digest, feeds, test.value, true)
					case "insert":
						catalog, activateErr := store.Activate(digest, feeds, 1, true)
						if activateErr != nil {
							t.Fatal(activateErr)
						}
						_, err = store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, 0), test.value)
					}
					if (err != nil) != test.wantErr {
						t.Fatalf("%s retention %d error = %v, wantErr %t", operation, test.value, err, test.wantErr)
					}
					if test.wantErr && err.Error() != "retention is out of range" {
						t.Fatalf("%s retention %d error = %v", operation, test.value, err)
					}
				})
			}
		})
	}
}

func TestHistoryPageSizeBoundaries(t *testing.T) {
	t.Parallel()
	store := newOwnershipStore(t)
	feeds, digest := testPlans(t, "page-size")
	catalog, err := store.Activate(digest, feeds, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Insert(testAggregate(feeds[0], catalog.ActivationGeneration, 0), 1); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value   uint32
		wantErr bool
	}{
		{value: 0, wantErr: true},
		{value: 1},
		{value: 50},
		{value: 51, wantErr: true},
	} {
		t.Run(fmt.Sprintf("%d", test.value), func(t *testing.T) {
			_, err := store.History("BTC/USD", test.value, nil)
			if (err != nil) != test.wantErr {
				t.Fatalf("History page size %d error = %v, wantErr %t", test.value, err, test.wantErr)
			}
			if test.wantErr && err.Error() != "invalid page size" {
				t.Fatalf("History page size %d error = %v", test.value, err)
			}
		})
	}
}

func ownershipAggregate(configured, successful uint32, contributors []string) domain.Aggregate {
	now := time.Unix(1_800_000_000, 0).UTC()
	return domain.Aggregate{
		Symbol:               "BTC/USD",
		Value:                "10.000000000000000000",
		Sequence:             1,
		ActivationGeneration: 1,
		CycleStartedAt:       now,
		CollectedAt:          now.Add(time.Millisecond),
		ConfiguredSources:    configured,
		SuccessfulSources:    successful,
		ContributorIDs:       contributors,
	}
}

func ownershipContributorIDs(count int) []string {
	contributors := make([]string, count)
	for i := range contributors {
		contributors[i] = fmt.Sprintf("source-%03d", i)
	}
	return contributors
}

func newOwnershipStore(t *testing.T) *Store {
	t.Helper()
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
	t.Cleanup(func() { closeTestResource(t, store) })
	return store
}
