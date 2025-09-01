package watcher

import (
	"context"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// 임시 한글 주석
// 체인의 이벤트를 모니터링하고 발생한 이벤트를 가져오는 단일 목적 인스턴스
type Watcher struct {
	log     log.Logger
	eventCh chan coretypes.ResultEvent
}

func New(
	ctx context.Context,
	logger log.Logger,
	subsClient *http.HTTP,
	chanSize int,
) *Watcher {
	s := &Watcher{
		log:     logger,
		eventCh: make(chan coretypes.ResultEvent, chanSize),
	}

	queries := []string{
		types.RegisterQuery,
		types.UpdateQuery,
		types.CompleteQuery,
	}

	for _, query := range queries {
		// 마지막 구독만 실패할 수 있기 때문에, 하나라도 실패하면 상위 객체에서 강제 재시작을 통해 ctx.Done()을 발생시켜야 함
		if err := s.subscribe(ctx, subsClient, query, chanSize); err != nil {
			return nil
		}
	}

	go func() {
		<-ctx.Done()
		subsClient.UnsubscribeAll(ctx, "")
		close(s.eventCh)
		s.log.Info("watcher stopped")
	}()

	s.log.Info("watcher started")

	return s
}

func (s *Watcher) EventCh() <-chan coretypes.ResultEvent { return s.eventCh }

func (s *Watcher) subscribe(ctx context.Context, subsClient *http.HTTP, query string, chanSize int) error {
	ch, err := subsClient.Subscribe(ctx, "", query, chanSize)
	if err != nil {
		s.log.Error("subscribe failed", "query", query)
		return err
	}
	s.log.Info("subscribe started", "query", query)

	go func() {
		for {
			select {
			case <-ctx.Done():
				s.log.Info("subscribe stopped", "query", query)
				return

			case event, ok := <-ch:
				if !ok {
					s.log.Info("subscribe closed", "query", query)
					return
				}

				s.eventCh <- event
			}
		}
	}()

	return nil
}
