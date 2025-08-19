// Package worker provides Oracle job processing and worker pool management
// Handles concurrent execution of Oracle data fetching tasks
package worker

import (
	"context"
	"runtime"
	"slices"
	"strconv"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/config"
	"github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	"github.com/creachadair/taskgroup"
	cmap "github.com/orcaman/concurrent-map/v2"
)

// WorkerPool manages concurrent Oracle job execution
// Provides job scheduling, result collection, and HTTP client management
type WorkerPool struct {
	logger      log.Logger                                   // Logger for worker operations
	baseCtx     context.Context                              // Base context for cancellation
	jobStore    cmap.ConcurrentMap[string, *types.OracleJob] // Thread-safe job storage
	resultCh    chan *types.OracleJobResult                  // Channel for job results
	workerFunc  taskgroup.StartFunc                          // Worker function for task execution
	workerGroup *taskgroup.Group                             // Goroutine management
	client      *httpClient                                  // HTTP client for data fetching
}

// New creates a new worker pool with configured concurrency and HTTP client
// Sets up job storage, result channel, and worker goroutine management
func New(logger log.Logger, baseCtx context.Context) *WorkerPool {
	wp := new(WorkerPool)
	wp.logger = logger
	wp.baseCtx = baseCtx

	// Initialize concurrent job storage and result channel
	wp.jobStore = cmap.New[*types.OracleJob]()
	wp.resultCh = make(chan *types.OracleJobResult, config.ChannelSize())

	// Setup worker group with CPU-based concurrency limit
	wp.workerGroup, wp.workerFunc = taskgroup.New(nil).Limit(2 * runtime.NumCPU())

	// Create HTTP client for external data fetching
	wp.client = newClient(wp.logger)

	return wp
}

// ProcessRequestDoc converts Oracle request document into executable job
// Determines endpoint assignment and creates job with proper nonce tracking
func (wp *WorkerPool) ProcessRequestDoc(requestDoc oracletypes.OracleRequestDoc) {
	// Find this Oracle's position in the account list
	index := slices.Index(requestDoc.AccountList, config.Address().String())
	if index == -1 {
		wp.logger.Error("request document not assigned to this oracle instance", "address", config.Address().String())
		return
	} else {
		// Use next endpoint to distribute load among Oracles
		index = (index + 1) % len(requestDoc.AccountList)
	}

	// Determine current nonce from existing job or request document
	var currentNonce uint64
	reqID := strconv.FormatUint(requestDoc.Nonce, 10)
	if job, ok := wp.jobStore.Get(reqID); ok {
		currentNonce = job.Nonce
	} else {
		currentNonce = requestDoc.Nonce
	}

	// Create Oracle job from request document
	job := &types.OracleJob{
		ID:     requestDoc.RequestId,
		URL:    requestDoc.Endpoints[index].Url,
		Path:   requestDoc.Endpoints[index].ParseRule,
		Nonce:  max(currentNonce, requestDoc.Nonce),
		Delay:  time.Duration(requestDoc.Period) * time.Second,
		Status: requestDoc.Status,
	}

	// Only process enabled requests
	if job.Status != oracletypes.RequestStatus_REQUEST_STATUS_ENABLED {
		wp.logger.Info("request document is not enabled", "request_id", reqID)
		return
	}

	wp.executeJob(job)
}

// ProcessComplete updates job nonce based on completion events from blockchain
// Ensures job execution continues with correct nonce after other Oracle submissions
func (wp *WorkerPool) ProcessComplete(reqID string, nonce uint64) {
	// Get existing job from storage
	job, ok := wp.jobStore.Get(reqID)
	if !ok {
		return
	}

	// Update nonce to latest completed value
	job.Nonce = max(job.Nonce, nonce)

	// Re-execute job with updated nonce
	wp.executeJob(job)
}

// Results returns a read-only channel for receiving completed Oracle job results
// Used by daemon to get results for blockchain submission
func (wp *WorkerPool) Results() <-chan *types.OracleJobResult {
	return wp.resultCh
}

// executeJob runs a single Oracle job in a managed goroutine
// Handles data fetching, parsing, extraction, and result submission
func (wp *WorkerPool) executeJob(job *types.OracleJob) {
	task := job

	// Execute job in worker goroutine with proper error handling
	wp.workerFunc(func() error {
		reqID := strconv.FormatUint(task.ID, 10)

		// Increment nonce for new execution
		if stored, ok := wp.jobStore.Get(reqID); ok {
			job.Nonce = stored.Nonce + 1
		} else {
			job.Nonce++
		}

		// Update job store with new nonce
		wp.jobStore.Set(reqID, job)

		// Fetch raw data from external API
		rawData, err := wp.client.fetchRawData(job.URL)
		if err != nil {
			wp.logger.Error("failed to fetch raw data", "error", err)
			return err
		}

		// Parse JSON response
		jsonData, err := wp.client.parseRawData(rawData)
		if err != nil {
			wp.logger.Error("failed to parse raw data", "error", err)
			return err
		}

		// Extract specific data using configured path
		result, err := wp.client.extractDataByPath(jsonData, job.Path)
		if err != nil {
			wp.logger.Error("failed to extract data by path", "error", err)
			return err
		}

		// Send result to channel for blockchain submission
		wp.resultCh <- &types.OracleJobResult{
			ID:    job.ID,
			Data:  result,
			Nonce: job.Nonce,
		}

		return nil
	})
}
