package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxFeeds             = 256
	MinSourcesPerFeed    = 3
	MaxSourcesPerFeed    = 64
	MaxSymbolBytes       = 128
	MaxValueBytes        = 256
	MaxNumericToken      = 256
	MaxCoefficientDigits = 96
	MaxAbsoluteExponent  = 256

	collectorSemanticPolicyVersion uint32 = 1
)

type SourcePlan struct {
	ID          string
	URL         string
	JSONPointer string
}

type FeedPlan struct {
	Symbol      string
	Interval    time.Duration
	StaleAfter  time.Duration
	Sources     []SourcePlan
	Fingerprint [32]byte
}

type CollectorPolicy struct {
	MaxConcurrency        uint32
	SourceResponseBytes   uint32
	MaxRedirects          uint32
	MaxAttempts           uint32
	RequestTimeout        time.Duration
	ConnectTimeout        time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	RetryInitialBackoff   time.Duration
	RetryMaxBackoff       time.Duration
}

type Aggregate struct {
	Symbol               string
	Value                string
	Sequence             uint64
	ActivationGeneration uint64
	CycleStartedAt       time.Time
	CollectedAt          time.Time
	ConfiguredSources    uint32
	SuccessfulSources    uint32
	ContributorIDs       []string
	FeedPlanFingerprint  [32]byte
}

type Freshness string

const (
	FreshnessNoValue      Freshness = "no_value"
	FreshnessFresh        Freshness = "fresh"
	FreshnessStale        Freshness = "stale"
	FreshnessClockAnomaly Freshness = "clock_anomaly"
)

type CycleActivity string

const (
	CycleIdle     CycleActivity = "idle"
	CycleInFlight CycleActivity = "in_flight"
)

type CycleOutcome string

const (
	CycleNever       CycleOutcome = "never"
	CycleFull        CycleOutcome = "full"
	CycleQuorum      CycleOutcome = "quorum"
	CycleUnderQuorum CycleOutcome = "under_quorum"
	CycleCancelled   CycleOutcome = "cancelled"
)

func NormalizeSymbol(symbol string) (string, error) {
	if !utf8.ValidString(symbol) {
		return "", errors.New("symbol is not valid UTF-8")
	}
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return "", errors.New("symbol is empty")
	}
	if len(normalized) > MaxSymbolBytes {
		return "", fmt.Errorf("symbol exceeds %d bytes", MaxSymbolBytes)
	}
	return normalized, nil
}

func CanonicalPlans(feeds []FeedPlan, policy CollectorPolicy) ([]FeedPlan, [32]byte, error) {
	canonical := make([]FeedPlan, len(feeds))
	copy(canonical, feeds)
	for i := range canonical {
		canonical[i].Sources = append([]SourcePlan(nil), canonical[i].Sources...)
		sort.Slice(canonical[i].Sources, func(a, b int) bool {
			if canonical[i].Sources[a].ID != canonical[i].Sources[b].ID {
				return canonical[i].Sources[a].ID < canonical[i].Sources[b].ID
			}
			if canonical[i].Sources[a].URL != canonical[i].Sources[b].URL {
				return canonical[i].Sources[a].URL < canonical[i].Sources[b].URL
			}
			return canonical[i].Sources[a].JSONPointer < canonical[i].Sources[b].JSONPointer
		})
		encoded, err := encodeFeedPlan(canonical[i], policy)
		if err != nil {
			return nil, [32]byte{}, err
		}
		canonical[i].Fingerprint = sha256.Sum256(encoded)
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Symbol < canonical[j].Symbol })

	var all bytes.Buffer
	writeUint32(&all, collectorSemanticPolicyVersion)
	writeUint32(&all, uint32(len(canonical)))
	for _, feed := range canonical {
		writeString(&all, feed.Symbol)
		all.Write(feed.Fingerprint[:])
	}
	return canonical, sha256.Sum256(all.Bytes()), nil
}

func encodeFeedPlan(feed FeedPlan, policy CollectorPolicy) ([]byte, error) {
	if feed.Interval <= 0 || feed.StaleAfter <= 0 {
		return nil, errors.New("feed durations must be positive")
	}
	var out bytes.Buffer
	writeUint32(&out, collectorSemanticPolicyVersion)
	writeString(&out, feed.Symbol)
	writeInt64(&out, int64(feed.Interval))
	writeInt64(&out, int64(feed.StaleAfter))
	writeUint32(&out, policy.MaxConcurrency)
	writeUint32(&out, policy.SourceResponseBytes)
	writeUint32(&out, policy.MaxRedirects)
	writeUint32(&out, policy.MaxAttempts)
	writeInt64(&out, int64(policy.RequestTimeout))
	writeInt64(&out, int64(policy.ConnectTimeout))
	writeInt64(&out, int64(policy.TLSHandshakeTimeout))
	writeInt64(&out, int64(policy.ResponseHeaderTimeout))
	writeInt64(&out, int64(policy.RetryInitialBackoff))
	writeInt64(&out, int64(policy.RetryMaxBackoff))
	writeUint32(&out, MaxNumericToken)
	writeUint32(&out, MaxCoefficientDigits)
	writeUint32(&out, MaxAbsoluteExponent)
	writeUint32(&out, uint32(len(feed.Sources)))
	for _, source := range feed.Sources {
		writeString(&out, source.ID)
		writeString(&out, source.URL)
		writeString(&out, source.JSONPointer)
	}
	return out.Bytes(), nil
}

func writeString(out *bytes.Buffer, value string) {
	writeUint32(out, uint32(len(value)))
	out.WriteString(value)
}

func writeUint32(out *bytes.Buffer, value uint32) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	out.Write(encoded[:])
}

func writeInt64(out *bytes.Buffer, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	out.Write(encoded[:])
}
