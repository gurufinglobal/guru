package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

func TestStateConcurrentReadersAndWriters(t *testing.T) {
	pair, catalog, latest := serviceFixture(t, time.Now().UTC())
	state := NewState(pair, catalog, latest, time.Now())
	start := make(chan struct{})
	errs := make(chan error, 5)
	var group sync.WaitGroup

	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for range 2_000 {
				if values := state.Fresh([]string{"BTC/USD"}); len(values) != 1 {
					status, _ := state.Status()
					errs <- fmt.Errorf("fresh values = %d, status = %#v", len(values), status.Feeds)
					return
				}
				status, _ := state.Status()
				if len(status.Feeds) != 1 {
					errs <- fmt.Errorf("status feeds = %d", len(status.Feeds))
					return
				}
			}
		}()
	}

	group.Add(1)
	go func() {
		defer group.Done()
		<-start
		record := latest["BTC/USD"]
		for i := 0; i < 2_000; i++ {
			state.CycleStarted(record.Symbol)
			if i%2 == 0 {
				state.CycleFinished(record.Symbol, domain.CycleFull, 3, time.Now())
				continue
			}
			record.Sequence++
			record.CollectedAt = time.Now()
			record.ContributorIDs = []string{"a", "b", "c"}
			state.AggregateCommitted(record, domain.CycleFull)
		}
	}()

	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestStateClonesContributorIDsAtOwnershipIngress(t *testing.T) {
	pair, catalog, latest := serviceFixture(t, time.Now().UTC())
	initial := latest["BTC/USD"]
	state := NewState(pair, catalog, latest, time.Now())
	initial.ContributorIDs[0] = "mutated-constructor-input"

	committed := latest["BTC/USD"]
	committed.Sequence++
	committed.ContributorIDs = []string{"a", "b", "c"}
	state.AggregateCommitted(committed, domain.CycleFull)
	committed.ContributorIDs[0] = "mutated-commit-input"

	state.mu.RLock()
	stored := append([]string(nil), state.feeds[0].latest.record.ContributorIDs...)
	state.mu.RUnlock()
	if stored[0] != "a" {
		t.Fatalf("stored contributor IDs alias caller input: %v", stored)
	}
}
