package scheduler

import (
	"context"
	"runtime"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/creachadair/taskgroup"
	cmap "github.com/orcaman/concurrent-map/v2"
)

// TODO: 기존 doc 불러오기
// TODO: event -> (doc) -> job
type Scheduler struct {
	log         log.Logger
	jobs        cmap.ConcurrentMap[string, *types.OracleJob]
	resultCh    chan *types.OracleJobResult
	workerFunc  taskgroup.StartFunc
	workerGroup *taskgroup.Group
}

func New(ctx context.Context, log log.Logger, queryClient oracletypes.QueryClient, chanSize int) *Scheduler {
	s := &Scheduler{
		log:      log,
		jobs:     cmap.New[*types.OracleJob](),
		resultCh: make(chan *types.OracleJobResult, chanSize),
	}
	s.workerGroup, s.workerFunc = taskgroup.New(nil).Limit(2 * runtime.NumCPU())

	docs := s.LoadOracleRequestDoc(ctx, queryClient)
	for _, doc := range docs {
		s.workerFunc(func() error {
			if job := s.temp_req2job(*doc); job != nil {
				s.ProcessJob(job)
			}

			return nil
		})
	}

	go func() {
		<-ctx.Done()
		s.workerGroup.Wait()
		close(s.resultCh)
	}()

	return s
}

func (s *Scheduler) LoadOracleRequestDoc(ctx context.Context, queryClient oracletypes.QueryClient) []*oracletypes.OracleRequestDoc {
	req := &oracletypes.QueryOracleRequestDocsRequest{
		Status: oracletypes.RequestStatus_REQUEST_STATUS_ENABLED,
	}
	res, err := queryClient.OracleRequestDocs(ctx, req)
	if err != nil {
		s.log.Error("failed to load oracle request doc", "error", err)
		return nil
	}

	s.log.Info("loaded oracle request doc", "doc", res.OracleRequestDocs)

	return res.OracleRequestDocs
}

func (s *Scheduler) ProcessEvent(event coretypes.ResultEvent) {
	s.workerFunc(func() error {
		if job := s.convertJob(event); job != nil {
			s.ProcessJob(job)
		}

		return nil
	})
}

func (s *Scheduler) ProcessJob(job *types.OracleJob) {
}

func (s *Scheduler) convertJob(event coretypes.ResultEvent) *types.OracleJob {
	var job *types.OracleJob

	switch event.Query {
	case types.RegisterQuery, types.UpdateQuery:
		s.log.Info("register or update query", "event", event)
		req := event.Data.(oracletypes.OracleRequestDoc) // query로 가져오기
		job = s.temp_req2job(req)
	case types.CompleteQuery:
		s.log.Info("complete query", "event", event)
		job = s.temp_complete2job(event)
	default:
		s.log.Info("unknown query", "query", event.Query)
		job = nil
	}

	return job
}

func (s *Scheduler) temp_req2job(req oracletypes.OracleRequestDoc) *types.OracleJob {
	return nil
}

func (s *Scheduler) temp_complete2job(event coretypes.ResultEvent) *types.OracleJob {
	return nil
}
