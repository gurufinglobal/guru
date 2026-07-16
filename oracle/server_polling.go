package oracle

import (
	"context"
	"log"
	"strings"
	"time"

	oraclev1 "github.com/gurufinglobal/guru/v3/api/guru/oracle/v1"
	"golang.org/x/sync/errgroup"
)

func (s *Sidecar) runSourcePollers(ctx context.Context, activeTasks []*oraclev1.OracleTask) error {
	sourceTimeout, err := s.config.SourceTimeoutDuration()
	if err != nil {
		return err
	}

	group, pollCtx := errgroup.WithContext(ctx)
	pollers := 0
	for _, task := range activeTasks {
		for _, source := range s.matchingSources(task) {
			if _, ok, err := source.IntervalDuration(); err != nil {
				return err
			} else if !ok {
				continue
			}

			task := task
			source := source
			group.Go(func() error {
				return s.pollSource(pollCtx, source, task, sourceTimeout)
			})
			pollers++
		}
	}
	log.Printf("oracle sidecar source pollers started count=%d", pollers)

	return group.Wait()
}

func (s *Sidecar) pollSource(ctx context.Context, source SourceConfig, task *oraclev1.OracleTask, sourceTimeout time.Duration) error {
	interval, ok, err := source.IntervalDuration()
	if err != nil || !ok {
		return err
	}
	log.Printf(
		"oracle sidecar source poller started symbol=%q source=%q interval=%s",
		normalizeSymbol(task.GetSymbol()),
		strings.TrimSpace(source.Name),
		interval,
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			sample, err := s.fetchAndCache(ctx, source, task, sourceTimeout)
			if err != nil {
				log.Printf(
					"oracle sidecar source poll failed symbol=%q source=%q err=%v",
					normalizeSymbol(task.GetSymbol()),
					strings.TrimSpace(source.Name),
					err,
				)
			} else {
				log.Printf(
					"oracle sidecar source polled symbol=%q source=%q value=%q",
					normalizeSymbol(task.GetSymbol()),
					sample.GetSource(),
					sample.GetValue(),
				)
			}
			timer.Reset(interval)
		}
	}
}
