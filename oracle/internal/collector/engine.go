package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
)

type CommitFunc func(domain.Aggregate) (domain.Aggregate, error)

type Observer interface {
	CycleStarted(symbol string)
	CycleFinished(symbol string, outcome domain.CycleOutcome, successfulSources uint32, completedAt time.Time)
	AggregateCommitted(record domain.Aggregate, outcome domain.CycleOutcome)
}

type Engine struct {
	feeds      []domain.FeedPlan
	generation uint64
	client     *SourceClient
	semaphore  chan struct{}
	commit     CommitFunc
	observer   Observer
}

func NewEngine(
	feeds []domain.FeedPlan,
	generation uint64,
	maxConcurrency uint32,
	client *SourceClient,
	commit CommitFunc,
	observer Observer,
) *Engine {
	return &Engine{
		feeds:      append([]domain.FeedPlan(nil), feeds...),
		generation: generation,
		client:     client,
		semaphore:  make(chan struct{}, maxConcurrency),
		commit:     commit,
		observer:   observer,
	}
}

func (e *Engine) Run(ctx context.Context) error {
	if len(e.feeds) == 0 {
		<-ctx.Done()
		return nil
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, len(e.feeds))
	var group sync.WaitGroup
	for _, feed := range e.feeds {
		feed := feed
		group.Add(1)
		go func() {
			defer group.Done()
			if err := e.runFeed(child, feed); err != nil {
				select {
				case errs <- err:
				default:
				}
				cancel()
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	case err := <-errs:
		cancel()
		<-done
		return err
	case <-ctx.Done():
		cancel()
		<-done
		return nil
	}
}

func (e *Engine) runFeed(ctx context.Context, feed domain.FeedPlan) error {
	nominal := time.Now()
	for {
		if ctx.Err() != nil {
			return nil
		}
		e.observer.CycleStarted(feed.Symbol)
		if err := e.runCycle(ctx, feed, nominal); err != nil {
			return fmt.Errorf("feed %s: %w", feed.Symbol, err)
		}
		nominal = nextNominal(nominal.Add(feed.Interval), time.Now(), feed.Interval)
		timer := time.NewTimer(time.Until(nominal))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func nextNominal(nominal, now time.Time, interval time.Duration) time.Time {
	if nominal.After(now) {
		return nominal
	}
	behind := now.Sub(nominal)
	return now.Add(interval - behind%interval)
}

func (e *Engine) runCycle(parent context.Context, feed domain.FeedPlan, nominal time.Time) error {
	startedAt := time.Now()
	deadline := nominal.Add(feed.Interval)
	cycleContext, cancel := context.WithDeadline(parent, deadline)
	defer cancel()

	type result struct {
		id          string
		value       sdkmath.LegacyDec
		completedAt time.Time
		ok          bool
	}
	results := make(chan result, len(feed.Sources))
	var group sync.WaitGroup
admitSources:
	for _, source := range feed.Sources {
		select {
		case e.semaphore <- struct{}{}:
		case <-cycleContext.Done():
			break admitSources
		}
		if cycleContext.Err() != nil {
			<-e.semaphore
			break admitSources
		}
		source := source
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-e.semaphore }()
			value, err := e.client.Fetch(cycleContext, source)
			results <- result{
				id:          source.ID,
				value:       value,
				completedAt: time.Now(),
				ok:          err == nil,
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		cancel()
	case <-cycleContext.Done():
		cancel()
		<-done
	}
	close(results)
	completedAt := time.Now()

	if parent.Err() != nil {
		e.observer.CycleFinished(feed.Symbol, domain.CycleCancelled, 0, completedAt)
		return nil
	}
	values := make([]sdkmath.LegacyDec, 0, len(feed.Sources))
	contributors := make([]string, 0, len(feed.Sources))
	for result := range results {
		if result.ok && result.completedAt.Before(deadline) {
			values = append(values, result.value)
			contributors = append(contributors, result.id)
		}
	}
	sort.Strings(contributors)
	successful := uint32(len(values))
	required := uint32(len(feed.Sources)/2 + 1)
	if successful < required {
		e.observer.CycleFinished(feed.Symbol, domain.CycleUnderQuorum, successful, completedAt)
		return nil
	}
	median, err := domain.Median(values)
	if err != nil {
		return err
	}
	outcome := domain.CycleQuorum
	if successful == uint32(len(feed.Sources)) {
		outcome = domain.CycleFull
	}
	candidate := domain.Aggregate{
		Symbol:               feed.Symbol,
		Value:                median.String(),
		ActivationGeneration: e.generation,
		CycleStartedAt:       startedAt,
		CollectedAt:          completedAt,
		ConfiguredSources:    uint32(len(feed.Sources)),
		SuccessfulSources:    successful,
		ContributorIDs:       contributors,
		FeedPlanFingerprint:  feed.Fingerprint,
	}
	committed, err := e.commit(candidate)
	if err != nil {
		return fmt.Errorf("commit aggregate: %w", err)
	}
	if committed.Sequence == 0 {
		return errors.New("committed aggregate has zero sequence")
	}
	e.observer.AggregateCommitted(committed, outcome)
	return nil
}
