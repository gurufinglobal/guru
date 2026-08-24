package domain

import (
	"testing"
	"time"
)

func TestCanonicalPlansIgnoreOrderingAndCoverEligibility(t *testing.T) {
	t.Parallel()
	policy := CollectorPolicy{
		MaxConcurrency:        2,
		SourceResponseBytes:   1024,
		MaxRedirects:          1,
		MaxAttempts:           2,
		RequestTimeout:        time.Second,
		ConnectTimeout:        time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		RetryInitialBackoff:   time.Millisecond,
		RetryMaxBackoff:       time.Second,
	}
	first := []FeedPlan{{
		Symbol:     "BTC/USD",
		Interval:   time.Second,
		StaleAfter: 2 * time.Second,
		Sources: []SourcePlan{
			{ID: "b", URL: "https://b.example", JSONPointer: "/v"},
			{ID: "a", URL: "https://a.example", JSONPointer: "/v"},
			{ID: "c", URL: "https://c.example", JSONPointer: "/v"},
		},
	}}
	second := []FeedPlan{{
		Symbol:     "BTC/USD",
		Interval:   time.Second,
		StaleAfter: 2 * time.Second,
		Sources: []SourcePlan{
			{ID: "c", URL: "https://c.example", JSONPointer: "/v"},
			{ID: "b", URL: "https://b.example", JSONPointer: "/v"},
			{ID: "a", URL: "https://a.example", JSONPointer: "/v"},
		},
	}}
	canonicalFirst, digestFirst, err := CanonicalPlans(first, policy)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSecond, digestSecond, err := CanonicalPlans(second, policy)
	if err != nil {
		t.Fatal(err)
	}
	if digestFirst != digestSecond || canonicalFirst[0].Fingerprint != canonicalSecond[0].Fingerprint {
		t.Fatal("ordering-only change altered fingerprint")
	}
	second[0].StaleAfter++
	_, changed, err := CanonicalPlans(second, policy)
	if err != nil {
		t.Fatal(err)
	}
	if changed == digestFirst {
		t.Fatal("eligibility change did not alter plan digest")
	}
}

func TestCanonicalPlansFingerprintCoversEveryMutableInput(t *testing.T) {
	t.Parallel()
	basePolicy := CollectorPolicy{
		MaxConcurrency:        2,
		SourceResponseBytes:   1024,
		MaxRedirects:          1,
		MaxAttempts:           2,
		RequestTimeout:        time.Second,
		ConnectTimeout:        time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		RetryInitialBackoff:   time.Millisecond,
		RetryMaxBackoff:       time.Second,
	}
	baseFeed := FeedPlan{
		Symbol:     "BTC/USD",
		Interval:   time.Second,
		StaleAfter: 2 * time.Second,
		Sources: []SourcePlan{
			{ID: "a", URL: "https://a.example", JSONPointer: "/v"},
			{ID: "b", URL: "https://b.example", JSONPointer: "/v"},
			{ID: "c", URL: "https://c.example", JSONPointer: "/v"},
		},
	}
	canonical, baseDigest, err := CanonicalPlans([]FeedPlan{baseFeed}, basePolicy)
	if err != nil {
		t.Fatal(err)
	}
	baseFingerprint := canonical[0].Fingerprint

	tests := []struct {
		name   string
		mutate func(*FeedPlan, *CollectorPolicy)
	}{
		{name: "symbol", mutate: func(feed *FeedPlan, _ *CollectorPolicy) { feed.Symbol = "ETH/USD" }},
		{name: "interval", mutate: func(feed *FeedPlan, _ *CollectorPolicy) { feed.Interval++ }},
		{name: "stale after", mutate: func(feed *FeedPlan, _ *CollectorPolicy) { feed.StaleAfter++ }},
		{name: "max concurrency", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.MaxConcurrency++ }},
		{name: "source response bytes", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.SourceResponseBytes++ }},
		{name: "max redirects", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.MaxRedirects++ }},
		{name: "max attempts", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.MaxAttempts++ }},
		{name: "request timeout", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.RequestTimeout++ }},
		{name: "connect timeout", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.ConnectTimeout++ }},
		{name: "TLS handshake timeout", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.TLSHandshakeTimeout++ }},
		{name: "response header timeout", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.ResponseHeaderTimeout++ }},
		{name: "retry initial backoff", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.RetryInitialBackoff++ }},
		{name: "retry max backoff", mutate: func(_ *FeedPlan, policy *CollectorPolicy) { policy.RetryMaxBackoff++ }},
		{name: "source ID", mutate: func(feed *FeedPlan, _ *CollectorPolicy) { feed.Sources[0].ID = "changed" }},
		{name: "source URL", mutate: func(feed *FeedPlan, _ *CollectorPolicy) { feed.Sources[0].URL = "https://changed.example" }},
		{name: "JSON pointer", mutate: func(feed *FeedPlan, _ *CollectorPolicy) { feed.Sources[0].JSONPointer = "/changed" }},
		{name: "source count", mutate: func(feed *FeedPlan, _ *CollectorPolicy) {
			feed.Sources = append(feed.Sources, SourcePlan{ID: "d", URL: "https://d.example", JSONPointer: "/v"})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			feed := baseFeed
			feed.Sources = append([]SourcePlan(nil), baseFeed.Sources...)
			policy := basePolicy
			test.mutate(&feed, &policy)
			canonical, digest, err := CanonicalPlans([]FeedPlan{feed}, policy)
			if err != nil {
				t.Fatal(err)
			}
			if digest == baseDigest || canonical[0].Fingerprint == baseFingerprint {
				t.Fatal("input change did not alter feed fingerprint and plan digest")
			}
		})
	}
}
