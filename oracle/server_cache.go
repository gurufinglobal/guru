package oracle

import (
	"context"
	"strings"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"google.golang.org/protobuf/proto"
)

type cachedSample struct {
	sample    *oraclev1.OracleSample
	fetchedAt time.Time
}

func (s *Sidecar) sampleForSource(ctx context.Context, source SourceConfig, task *oraclev1.OracleTask, sourceTimeout time.Duration) (*oraclev1.OracleSample, error) {
	interval, ok, err := source.IntervalDuration()
	if err != nil {
		return nil, err
	}
	if ok {
		if sample, found := s.cachedSourceSample(source, task, interval); found {
			return sample, nil
		}

		return s.fetchAndCache(ctx, source, task, sourceTimeout)
	}

	return s.client.Fetch(ctx, source, task, sourceTimeout)
}

func (s *Sidecar) cachedSourceSample(source SourceConfig, task *oraclev1.OracleTask, interval time.Duration) (*oraclev1.OracleSample, bool) {
	key := sourceCacheKey(source, task)
	now := s.client.now()

	s.cacheMu.RLock()
	cached, found := s.sourceCache[key]
	s.cacheMu.RUnlock()
	if !found || now.Sub(cached.fetchedAt) > interval {
		return nil, false
	}

	return cloneOracleSample(cached.sample), true
}

func (s *Sidecar) fetchAndCache(ctx context.Context, source SourceConfig, task *oraclev1.OracleTask, sourceTimeout time.Duration) (*oraclev1.OracleSample, error) {
	key := sourceCacheKey(source, task)
	result, err, _ := s.fetchGroup.Do(key, func() (any, error) {
		return s.client.Fetch(ctx, source, task, sourceTimeout)
	})
	if err != nil {
		return nil, err
	}
	sample := result.(*oraclev1.OracleSample)

	s.cacheMu.Lock()
	s.sourceCache[key] = cachedSample{
		sample:    cloneOracleSample(sample),
		fetchedAt: s.client.now(),
	}
	s.cacheMu.Unlock()

	return cloneOracleSample(sample), nil
}

func sourceCacheKey(source SourceConfig, task *oraclev1.OracleTask) string {
	return normalizeSymbol(task.GetSymbol()) + "\x00" + strings.TrimSpace(source.Name)
}

func cloneOracleSample(sample *oraclev1.OracleSample) *oraclev1.OracleSample {
	if sample == nil {
		return nil
	}

	return proto.Clone(sample).(*oraclev1.OracleSample)
}
