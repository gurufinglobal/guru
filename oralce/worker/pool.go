package worker

import (
	"context"
	"runtime"
	"slices"
	"strconv"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/v2/oralce/config"
	"github.com/GPTx-global/guru-v2/v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/v2/x/oracle/types"
	"github.com/creachadair/taskgroup"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type WorkerPool struct {
	logger      log.Logger
	jobStore    cmap.ConcurrentMap[string, *types.OracleJob]
	resultCh    chan *types.OracleJobResult
	workerFunc  taskgroup.StartFunc
	workerGroup *taskgroup.Group
	client      *httpClient
}

func New(ctx context.Context, logger log.Logger) *WorkerPool {
	wp := new(WorkerPool)
	wp.logger = logger

	wp.jobStore = cmap.New[*types.OracleJob]()
	wp.resultCh = make(chan *types.OracleJobResult, config.ChannelSize())

	wp.workerGroup, wp.workerFunc = taskgroup.New(nil).Limit(2 * runtime.NumCPU())
	go func() {
		<-ctx.Done()
		wp.workerGroup.Wait()
		close(wp.resultCh)
	}()

	wp.client = newHTTPClient(wp.logger)

	return wp
}

// ProcessRequestDoc maps an Oracle request document to a scheduled job.
// It selects the endpoint for this instance and computes initial delay.
func (wp *WorkerPool) ProcessRequestDoc(ctx context.Context, requestDoc oracletypes.OracleRequestDoc, timestamp uint64) {
	if requestDoc.Status != oracletypes.RequestStatus_REQUEST_STATUS_ENABLED {
		wp.logger.Info("request document is not enabled", "request_id", requestDoc.RequestId)
		return
	}

	index := slices.Index(requestDoc.AccountList, config.Address().String())
	if index == -1 {
		wp.logger.Info("request document not assigned to this oracle instance")
		reqID := strconv.FormatUint(requestDoc.RequestId, 10)
		_, ok := wp.jobStore.Get(reqID)
		if ok {
			wp.jobStore.Remove(reqID)
			wp.logger.Info("removed job from store", "request_id", requestDoc.RequestId)
		}
		return
	} else {
		length := len(requestDoc.Endpoints)
		if length == 0 {
			wp.logger.Info("no endpoints found", "request_id", requestDoc.RequestId)
			return
		}

		index %= len(requestDoc.Endpoints)
	}

	var currentNonce uint64
	requestIDStr := strconv.FormatUint(requestDoc.RequestId, 10)
	if job, ok := wp.jobStore.Get(requestIDStr); ok {
		currentNonce = job.Nonce
	} else {
		currentNonce = requestDoc.Nonce
	}

	periodSec := uint64(requestDoc.Period)
	nowSec := uint64(time.Now().Unix())
	tsSec := uint64(timestamp)
	dsec := int64(tsSec+periodSec) - int64(nowSec)

	job := &types.OracleJob{
		ID:     requestDoc.RequestId,
		URL:    requestDoc.Endpoints[index].Url,
		Path:   requestDoc.Endpoints[index].ParseRule,
		Nonce:  max(currentNonce, requestDoc.Nonce),
		Delay:  time.Duration(max(int64(0), dsec)) * time.Second,
		Period: time.Duration(requestDoc.Period),
		Status: requestDoc.Status,
	}

	wp.executeJob(ctx, job)
}

// ProcessComplete updates a job state using on-chain completion event data.
// It advances the nonce and reschedules the next execution based on block time.
func (wp *WorkerPool) ProcessComplete(ctx context.Context, reqID string, nonce uint64, timestamp uint64) {
	job, ok := wp.jobStore.Get(reqID)
	if !ok {
		wp.logger.Debug("job not found", "request_id", reqID)
		return
	}

	job.Nonce = max(job.Nonce, nonce)
	periodSec := uint64(job.Period)
	nowSec := uint64(time.Now().Unix())
	tsSec := uint64(timestamp)
	dsec := int64(tsSec+periodSec) - int64(nowSec)
	job.Delay = time.Duration(max(int64(0), dsec)) * time.Second

	wp.executeJob(ctx, job)
}

// Results relturns a read-only channel of completed job results.
// The channe is closed when the worker pool is shut down.
func (wp *WorkerPool) Results() <-chan *types.OracleJobResult {
	return wp.resultCh
}

// executeJob schedules a single job execution in a worker goroutine.
// It honors ctx cancellation for delaying the first run.
func (wp *WorkerPool) executeJob(ctx context.Context, job *types.OracleJob) {
	task := job

	wp.workerFunc(func() error {
		if 0 < task.Nonce && 0 < task.Delay {
			select {
			case <-time.After(task.Delay):
			case <-ctx.Done():
				return nil
			}
		}

		reqID := strconv.FormatUint(task.ID, 10)

		if stored, ok := wp.jobStore.Get(reqID); ok {
			task.Nonce = stored.Nonce + 1
		} else {
			task.Nonce++
		}

		wp.jobStore.Set(reqID, task)

		rawData, err := wp.client.fetchRawData(task.URL)
		if err != nil {
			wp.logger.Error("failed to fetch raw data", "error", err)
			wp.resultCh <- nil
			return err
		}
		wp.logger.Debug("fetched raw data", "id", task.ID, "url", task.URL)

		jsonData, err := wp.client.parseRawData(rawData)
		if err != nil {
			wp.logger.Error("failed to parse raw data", "error", err)
			return err
		}

		result, err := wp.client.extractDataByPath(jsonData, task.Path)
		if err != nil {
			wp.logger.Error("failed to extract data by path", "error", err)
			return err
		}

		wp.resultCh <- &types.OracleJobResult{
			ID:    task.ID,
			Data:  result,
			Nonce: task.Nonce,
		}
		wp.logger.Debug("sent result to channel", "id", task.ID, "data", result)

		return nil
	})
}
