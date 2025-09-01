package submitter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SequenceManager_impl는 시퀀스 관리자의 구현체
type SequenceManager_impl struct {
	logger    log.Logger
	clientCtx ClientContext
	address   sdk.AccAddress

	// 시퀀스 상태
	mu           sync.RWMutex
	currentSeq   uint64
	lastSyncTime time.Time
	syncInterval time.Duration

	// 통계
	totalSyncs    int64
	syncErrors    int64
	sequenceJumps int64
}

// NewSequenceManager는 새로운 시퀀스 관리자를 생성
func NewSequenceManager(
	logger log.Logger,
	clientCtx ClientContext,
	syncInterval time.Duration,
) (SequenceManager, error) {

	address := clientCtx.GetFromAddress()
	if address.Empty() {
		return nil, fmt.Errorf("invalid address from client context")
	}

	manager := &SequenceManager_impl{
		logger:       logger,
		clientCtx:    clientCtx,
		address:      address,
		syncInterval: syncInterval,
	}

	// 초기 시퀀스 동기화
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := manager.SyncWithChain(ctx); err != nil {
		logger.Warn("failed to sync initial sequence", "error", err)
		// 초기 동기화 실패는 치명적이지 않음 (기본값 0 사용)
	}

	return manager, nil
}

// GetSequence는 현재 시퀀스 번호를 반환
func (sm *SequenceManager_impl) GetSequence() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.currentSeq
}

// NextSequence는 다음 시퀀스 번호를 반환하고 내부 카운터를 증가
func (sm *SequenceManager_impl) NextSequence() uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	next := sm.currentSeq
	sm.currentSeq++

	sm.logger.Debug("sequence incremented",
		"current", next,
		"next", sm.currentSeq)

	return next
}

// UpdateSequence는 시퀀스를 특정 값으로 업데이트
func (sm *SequenceManager_impl) UpdateSequence(sequence uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldSeq := sm.currentSeq
	sm.currentSeq = sequence

	if sequence > oldSeq+1 {
		sm.sequenceJumps++
		sm.logger.Info("sequence jump detected",
			"old", oldSeq,
			"new", sequence,
			"jump", sequence-oldSeq)
	}

	sm.logger.Debug("sequence updated",
		"old", oldSeq,
		"new", sequence)
}

// SyncWithChain은 체인의 실제 시퀀스와 동기화
func (sm *SequenceManager_impl) SyncWithChain(ctx context.Context) error {
	accountRetriever := sm.clientCtx.GetAccountRetriever()
	if accountRetriever == nil {
		return fmt.Errorf("account retriever is nil")
	}

	// ClientContext를 client.Context로 변환 (어댑터 패턴)
	var clientCtx client.Context
	if adapter, ok := sm.clientCtx.(*DefaultClientContext); ok {
		clientCtx = adapter.clientCtx
	} else {
		return fmt.Errorf("invalid client context type")
	}

	// 체인에서 계정 정보 조회
	_, sequence, err := accountRetriever.GetAccountNumberSequence(
		clientCtx,
		sm.address,
	)
	if err != nil {
		sm.mu.Lock()
		sm.syncErrors++
		sm.mu.Unlock()

		sm.logger.Error("failed to get account sequence from chain",
			"address", sm.address.String(),
			"error", err)

		return &SequenceError{
			Message:  "failed to get sequence from chain",
			Original: err,
		}
	}

	// 시퀀스 업데이트
	sm.mu.Lock()
	oldSeq := sm.currentSeq
	sm.currentSeq = sequence
	sm.lastSyncTime = time.Now()
	sm.totalSyncs++
	sm.mu.Unlock()

	if oldSeq != sequence {
		sm.logger.Info("sequence synced with chain",
			"address", sm.address.String(),
			"old_sequence", oldSeq,
			"new_sequence", sequence)
	} else {
		sm.logger.Debug("sequence already in sync",
			"address", sm.address.String(),
			"sequence", sequence)
	}

	return nil
}

// HandleSequenceError는 시퀀스 관련 에러를 처리하고 복구를 시도
func (sm *SequenceManager_impl) HandleSequenceError(ctx context.Context, err error) error {
	sm.logger.Debug("handling sequence error", "error", err)

	// 트랜잭션 에러에서 시퀀스 정보 추출
	if txErr, ok := err.(*TransactionError); ok {
		return sm.handleTransactionSequenceError(ctx, txErr)
	}

	// 일반 시퀀스 에러 처리
	if seqErr, ok := err.(*SequenceError); ok {
		return sm.handleSequenceErrorDirect(ctx, seqErr)
	}

	// 체인과 동기화 시도
	syncErr := sm.SyncWithChain(ctx)
	if syncErr != nil {
		sm.logger.Error("failed to sync after sequence error",
			"original_error", err,
			"sync_error", syncErr)
		return fmt.Errorf("sequence error recovery failed: %w", err)
	}

	sm.logger.Info("sequence recovered via chain sync")
	return nil
}

// handleTransactionSequenceError는 트랜잭션 에러의 시퀀스 문제를 처리
func (sm *SequenceManager_impl) handleTransactionSequenceError(ctx context.Context, txErr *TransactionError) error {
	// Cosmos SDK의 일반적인 시퀀스 에러 코드들
	switch txErr.Code {
	case 32: // account sequence mismatch
		sm.logger.Info("account sequence mismatch detected",
			"tx_hash", txErr.TxHash,
			"raw_log", txErr.RawLog)

		// 체인과 동기화
		if err := sm.SyncWithChain(ctx); err != nil {
			return fmt.Errorf("failed to sync sequence after mismatch: %w", err)
		}

		return nil

	case 19: // tx already in mempool
		sm.logger.Debug("transaction already in mempool, incrementing sequence",
			"tx_hash", txErr.TxHash)

		// 시퀀스만 증가 (트랜잭션은 이미 제출됨)
		sm.NextSequence()
		return nil

	default:
		// 다른 에러는 일반적인 동기화로 처리
		return sm.SyncWithChain(ctx)
	}
}

// handleSequenceErrorDirect는 직접적인 시퀀스 에러를 처리
func (sm *SequenceManager_impl) handleSequenceErrorDirect(ctx context.Context, seqErr *SequenceError) error {
	sm.logger.Info("direct sequence error detected",
		"expected", seqErr.ExpectedSeq,
		"actual", seqErr.ActualSeq)

	// 예상 시퀀스가 더 크면 그 값으로 업데이트
	if seqErr.ExpectedSeq > seqErr.ActualSeq {
		sm.UpdateSequence(seqErr.ExpectedSeq)
		return nil
	}

	// 아니면 체인과 동기화
	return sm.SyncWithChain(ctx)
}

// ShouldSync는 동기화가 필요한지 확인
func (sm *SequenceManager_impl) ShouldSync() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return time.Since(sm.lastSyncTime) > sm.syncInterval
}

// GetStats는 시퀀스 관리자 통계를 반환
func (sm *SequenceManager_impl) GetStats() SequenceManagerStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return SequenceManagerStats{
		CurrentSequence: sm.currentSeq,
		LastSyncTime:    sm.lastSyncTime,
		TotalSyncs:      sm.totalSyncs,
		SyncErrors:      sm.syncErrors,
		SequenceJumps:   sm.sequenceJumps,
		SyncInterval:    sm.syncInterval,
	}
}

// StartAutoSync는 자동 동기화를 시작
func (sm *SequenceManager_impl) StartAutoSync(ctx context.Context) {
	if sm.syncInterval <= 0 {
		sm.logger.Debug("auto sync disabled (interval <= 0)")
		return
	}

	ticker := time.NewTicker(sm.syncInterval)
	defer ticker.Stop()

	sm.logger.Info("starting auto sequence sync",
		"interval", sm.syncInterval)

	for {
		select {
		case <-ctx.Done():
			sm.logger.Debug("auto sync stopped")
			return

		case <-ticker.C:
			if err := sm.SyncWithChain(ctx); err != nil {
				sm.logger.Error("auto sync failed", "error", err)
			}
		}
	}
}

// Reset은 시퀀스를 0으로 초기화하고 체인과 동기화
func (sm *SequenceManager_impl) Reset(ctx context.Context) error {
	sm.mu.Lock()
	sm.currentSeq = 0
	sm.mu.Unlock()

	sm.logger.Info("sequence reset to 0")

	return sm.SyncWithChain(ctx)
}

// 통계 구조체

// SequenceManagerStats는 시퀀스 관리자 통계를 나타내는 구조체
type SequenceManagerStats struct {
	CurrentSequence uint64        `json:"current_sequence"`
	LastSyncTime    time.Time     `json:"last_sync_time"`
	TotalSyncs      int64         `json:"total_syncs"`
	SyncErrors      int64         `json:"sync_errors"`
	SequenceJumps   int64         `json:"sequence_jumps"`
	SyncInterval    time.Duration `json:"sync_interval"`
}

// MockSequenceManager는 테스트용 모크 시퀀스 관리자
type MockSequenceManager struct {
	mu       sync.RWMutex
	sequence uint64
	syncErr  error
}

// NewMockSequenceManager는 새로운 모크 시퀀스 관리자를 생성
func NewMockSequenceManager(initialSequence uint64) *MockSequenceManager {
	return &MockSequenceManager{
		sequence: initialSequence,
	}
}

// GetSequence는 현재 시퀀스 번호를 반환
func (m *MockSequenceManager) GetSequence() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sequence
}

// NextSequence는 다음 시퀀스 번호를 반환하고 내부 카운터를 증가
func (m *MockSequenceManager) NextSequence() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := m.sequence
	m.sequence++
	return next
}

// UpdateSequence는 시퀀스를 특정 값으로 업데이트
func (m *MockSequenceManager) UpdateSequence(sequence uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sequence = sequence
}

// SyncWithChain은 체인의 실제 시퀀스와 동기화 (모크)
func (m *MockSequenceManager) SyncWithChain(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.syncErr
}

// HandleSequenceError는 시퀀스 관련 에러를 처리 (모크)
func (m *MockSequenceManager) HandleSequenceError(ctx context.Context, err error) error {
	// 간단한 모크: 항상 시퀀스 증가
	m.NextSequence()
	return nil
}

// SetSyncError는 동기화 에러를 설정 (테스트용)
func (m *MockSequenceManager) SetSyncError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.syncErr = err
}
