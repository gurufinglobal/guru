package service

import (
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/gurufinglobal/guru/oracle/internal/config"
	"github.com/gurufinglobal/guru/oracle/internal/domain"
	"github.com/gurufinglobal/guru/oracle/internal/storage"
)

type latestAnchor struct {
	record       domain.Aggregate
	anchor       time.Time
	initialAge   time.Duration
	clockAnomaly bool
}

type feedView struct {
	plan            domain.FeedPlan
	latest          *latestAnchor
	hasPrior        bool
	activity        domain.CycleActivity
	lastOutcome     domain.CycleOutcome
	hasLastCycle    bool
	lastCompletedAt time.Time
	lastSuccessful  uint32
}

type State struct {
	mu                  sync.RWMutex
	generation          uint64
	publicationRevision string
	sourcesSHA256       string
	feeds               []feedView
	bySymbol            map[string]int
	diagnostics         *Diagnostics
}

func NewState(pair *config.Pair, catalog storage.Catalog, latest map[string]domain.Aggregate, startup time.Time) *State {
	return newState(pair, catalog, latest, startup, nil)
}

func newState(
	pair *config.Pair,
	catalog storage.Catalog,
	latest map[string]domain.Aggregate,
	startup time.Time,
	diagnostics *Diagnostics,
) *State {
	feeds := make([]feedView, 0, len(pair.Feeds))
	for _, plan := range pair.Feeds {
		view := feedView{
			plan:        plan,
			activity:    domain.CycleIdle,
			lastOutcome: domain.CycleNever,
		}
		if record, ok := latest[plan.Symbol]; ok {
			if record.ActivationGeneration != catalog.ActivationGeneration ||
				record.FeedPlanFingerprint != plan.Fingerprint {
				view.hasPrior = true
			} else {
				age := startup.UTC().Sub(record.CollectedAt)
				anchor := &latestAnchor{record: cloneAggregate(record), anchor: startup, initialAge: age}
				if age < 0 {
					anchor.clockAnomaly = true
					anchor.initialAge = 0
				}
				view.latest = anchor
			}
		}
		feeds = append(feeds, view)
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].plan.Symbol < feeds[j].plan.Symbol })
	return &State{
		generation:          catalog.ActivationGeneration,
		publicationRevision: pair.Config.PublicationRevision,
		sourcesSHA256:       pair.Config.SourcesSHA256,
		feeds:               feeds,
		bySymbol:            indexFeeds(feeds),
		diagnostics:         diagnostics,
	}
}

func (s *State) CycleStarted(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index, ok := s.bySymbol[symbol]; ok {
		s.feeds[index].activity = domain.CycleInFlight
	}
}

func (s *State) CycleFinished(symbol string, outcome domain.CycleOutcome, successfulSources uint32, completedAt time.Time) {
	configuredSources := uint32(0)
	s.mu.Lock()
	if index, ok := s.bySymbol[symbol]; ok {
		view := &s.feeds[index]
		configuredSources = uint32(len(view.plan.Sources))
		view.activity = domain.CycleIdle
		view.lastOutcome = outcome
		view.hasLastCycle = true
		view.lastCompletedAt = completedAt
		view.lastSuccessful = successfulSources
	}
	s.mu.Unlock()
	s.diagnostics.CycleFinished(symbol, outcome, configuredSources, successfulSources)
}

func (s *State) AggregateCommitted(record domain.Aggregate, outcome domain.CycleOutcome) {
	now := time.Now()
	age := now.Sub(record.CollectedAt)
	clockAnomaly := false
	if age < 0 {
		age = 0
		clockAnomaly = true
	}
	record = cloneAggregate(record)
	s.mu.Lock()
	if index, ok := s.bySymbol[record.Symbol]; ok {
		view := &s.feeds[index]
		view.latest = &latestAnchor{
			record:       record,
			anchor:       now,
			initialAge:   age,
			clockAnomaly: clockAnomaly,
		}
		view.hasPrior = false
		view.activity = domain.CycleIdle
		view.lastOutcome = outcome
		view.hasLastCycle = true
		view.lastCompletedAt = record.CollectedAt
		view.lastSuccessful = record.SuccessfulSources
	}
	s.mu.Unlock()
	s.diagnostics.CycleFinished(
		record.Symbol,
		outcome,
		record.ConfiguredSources,
		record.SuccessfulSources,
	)
}

type ConsumerValue struct {
	Symbol      string
	Value       string
	SourceCount uint32
}

type consumerOmission struct {
	symbol string
	reason string
}

func (s *State) Fresh(symbols []string) []ConsumerValue {
	s.mu.RLock()
	result, omissions := s.freshLocked(symbols, time.Now())
	s.mu.RUnlock()
	s.reportOmissions(omissions)
	return result
}

func (s *State) freshAt(symbols []string, now time.Time) []ConsumerValue {
	s.mu.RLock()
	result, omissions := s.freshLocked(symbols, now)
	s.mu.RUnlock()
	s.reportOmissions(omissions)
	return result
}

func (s *State) freshLocked(symbols []string, now time.Time) ([]ConsumerValue, []consumerOmission) {
	var omissions []consumerOmission
	result := make([]ConsumerValue, 0, len(symbols))
	for _, symbol := range symbols {
		index, ok := s.bySymbol[symbol]
		if !ok {
			omissions = append(omissions, consumerOmission{symbol: symbol, reason: "unconfigured"})
			continue
		}
		feed := s.feeds[index]
		fresh := freshness(feed, now)
		if fresh != domain.FreshnessFresh {
			omissions = append(omissions, consumerOmission{symbol: symbol, reason: omissionReason(feed, fresh)})
			continue
		}
		result = append(result, ConsumerValue{
			Symbol:      symbol,
			Value:       feed.latest.record.Value,
			SourceCount: feed.latest.record.SuccessfulSources,
		})
	}
	return result, omissions
}

func (s *State) reportOmissions(omissions []consumerOmission) {
	for _, omitted := range omissions {
		s.diagnostics.Omitted(omitted.symbol, omitted.reason)
	}
}

type StatusData struct {
	Health               string       `json:"health"`
	PublicationRevision  string       `json:"publication_revision"`
	SourcesSHA256        string       `json:"sources_sha256"`
	ActivationGeneration string       `json:"activation_generation"`
	Feeds                []FeedStatus `json:"feeds"`
}

type FeedStatus struct {
	Symbol                string        `json:"symbol"`
	ActivationGeneration  string        `json:"activation_generation"`
	FeedPlanFingerprint   string        `json:"feed_plan_fingerprint"`
	ConfiguredSourceCount uint32        `json:"configured_source_count"`
	IntervalNanos         string        `json:"interval_nanos"`
	StaleAfterNanos       string        `json:"stale_after_nanos"`
	Freshness             string        `json:"freshness"`
	Health                string        `json:"health"`
	Latest                *LatestStatus `json:"latest"`
	Cycle                 CycleStatus   `json:"cycle"`
	OmissionReason        *string       `json:"omission_reason"`
}

type LatestStatus struct {
	Sequence              string `json:"sequence"`
	CollectedAt           string `json:"collected_at"`
	SuccessfulSourceCount uint32 `json:"successful_source_count"`
}

type CycleStatus struct {
	Activity              string  `json:"activity"`
	LastOutcome           string  `json:"last_outcome"`
	CompletedAt           *string `json:"completed_at"`
	SuccessfulSourceCount uint32  `json:"successful_source_count"`
}

func (s *State) Status() (StatusData, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	return s.statusLocked(now), now
}

func (s *State) statusAt(now time.Time) StatusData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusLocked(now)
}

func (s *State) statusLocked(now time.Time) StatusData {
	data := StatusData{
		PublicationRevision:  s.publicationRevision,
		SourcesSHA256:        s.sourcesSHA256,
		ActivationGeneration: uintString(s.generation),
		Feeds:                make([]FeedStatus, 0, len(s.feeds)),
	}
	healthy := 0
	available := 0
	for _, feed := range s.feeds {
		fresh := freshness(feed, now)
		health := healthFor(feed, fresh)
		if health == "healthy" {
			healthy++
		}
		if health != "unavailable" {
			available++
		}
		status := FeedStatus{
			Symbol:                feed.plan.Symbol,
			ActivationGeneration:  uintString(s.generation),
			FeedPlanFingerprint:   hex.EncodeToString(feed.plan.Fingerprint[:]),
			ConfiguredSourceCount: uint32(len(feed.plan.Sources)),
			IntervalNanos:         durationString(feed.plan.Interval),
			StaleAfterNanos:       durationString(feed.plan.StaleAfter),
			Freshness:             string(fresh),
			Health:                health,
			Cycle: CycleStatus{
				Activity:              string(feed.activity),
				LastOutcome:           string(feed.lastOutcome),
				SuccessfulSourceCount: feed.lastSuccessful,
			},
		}
		if feed.latest != nil && feed.latest.record.ActivationGeneration == s.generation &&
			feed.latest.record.FeedPlanFingerprint == feed.plan.Fingerprint {
			status.Latest = &LatestStatus{
				Sequence:              uintString(feed.latest.record.Sequence),
				CollectedAt:           feed.latest.record.CollectedAt.UTC().Format(time.RFC3339Nano),
				SuccessfulSourceCount: feed.latest.record.SuccessfulSources,
			}
		}
		if feed.hasLastCycle {
			completed := feed.lastCompletedAt.UTC().Format(time.RFC3339Nano)
			status.Cycle.CompletedAt = &completed
		}
		if reason := omissionReason(feed, fresh); reason != "" {
			status.OmissionReason = &reason
		}
		data.Feeds = append(data.Feeds, status)
	}
	switch {
	case len(s.feeds) == 0 || available == 0:
		data.Health = "unavailable"
	case healthy == len(s.feeds):
		data.Health = "healthy"
	default:
		data.Health = "degraded"
	}
	return data
}

func freshness(feed feedView, now time.Time) domain.Freshness {
	if feed.latest == nil {
		return domain.FreshnessNoValue
	}
	if feed.latest.clockAnomaly {
		return domain.FreshnessClockAnomaly
	}
	age := feed.latest.initialAge + now.Sub(feed.latest.anchor)
	if age < 0 {
		return domain.FreshnessClockAnomaly
	}
	if age >= feed.plan.StaleAfter {
		return domain.FreshnessStale
	}
	return domain.FreshnessFresh
}

func healthFor(feed feedView, current domain.Freshness) string {
	if current != domain.FreshnessFresh {
		return "unavailable"
	}
	if feed.lastOutcome == domain.CycleFull {
		return "healthy"
	}
	return "degraded"
}

func omissionReason(feed feedView, current domain.Freshness) string {
	switch current {
	case domain.FreshnessStale:
		return "stale"
	case domain.FreshnessClockAnomaly:
		return "clock_anomaly"
	case domain.FreshnessNoValue:
		if feed.hasPrior {
			return "prior_generation"
		}
		if feed.lastOutcome == domain.CycleUnderQuorum {
			return "under_quorum"
		}
		return "no_value"
	default:
		return ""
	}
}

func cloneAggregate(input domain.Aggregate) domain.Aggregate {
	input.ContributorIDs = append([]string(nil), input.ContributorIDs...)
	return input
}

func indexFeeds(feeds []feedView) map[string]int {
	result := make(map[string]int, len(feeds))
	for i, feed := range feeds {
		result[feed.plan.Symbol] = i
	}
	return result
}
