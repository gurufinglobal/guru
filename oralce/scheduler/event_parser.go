package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"cosmossdk.io/log"
	commontypes "github.com/GPTx-global/guru-v2/oralce/types"
	oracletypes "github.com/GPTx-global/guru-v2/x/oracle/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// EventParser_impl는 블록체인 이벤트 파서의 구현체
type EventParser_impl struct {
	logger      log.Logger
	queryClient QueryClient
}

// NewEventParser는 새로운 이벤트 파서를 생성
func NewEventParser(logger log.Logger, queryClient QueryClient) EventParser {
	return &EventParser_impl{
		logger:      logger,
		queryClient: queryClient,
	}
}

// ParseRegisterEvent는 등록 이벤트를 파싱
func (ep *EventParser_impl) ParseRegisterEvent(ctx context.Context, event coretypes.ResultEvent) (*oracletypes.OracleRequestDoc, error) {
	if !ep.IsEventSupported(event) {
		return nil, &EventParseError{
			EventType: "register",
			Query:     event.Query,
			Message:   "unsupported event type",
		}
	}

	// 이벤트에서 Request ID 추출
	requestID, err := ep.extractRequestID(event, commontypes.RegisterID)
	if err != nil {
		return nil, &EventParseError{
			EventType: "register",
			Query:     event.Query,
			Message:   "failed to extract request ID",
			Cause:     err,
		}
	}

	// 블록체인에서 실제 문서 조회
	doc, err := ep.fetchRequestDoc(ctx, requestID)
	if err != nil {
		return nil, &EventParseError{
			EventType: "register",
			Query:     event.Query,
			Message:   "failed to fetch request document",
			Cause:     err,
		}
	}

	ep.logger.Debug("register event parsed",
		"request_id", doc.RequestId,
		"nonce", doc.Nonce,
		"status", doc.Status)

	return doc, nil
}

// ParseUpdateEvent는 업데이트 이벤트를 파싱
func (ep *EventParser_impl) ParseUpdateEvent(ctx context.Context, event coretypes.ResultEvent) (*oracletypes.OracleRequestDoc, error) {
	if !ep.IsEventSupported(event) {
		return nil, &EventParseError{
			EventType: "update",
			Query:     event.Query,
			Message:   "unsupported event type",
		}
	}

	// 이벤트에서 Request ID 추출
	requestID, err := ep.extractRequestID(event, commontypes.UpdateID)
	if err != nil {
		return nil, &EventParseError{
			EventType: "update",
			Query:     event.Query,
			Message:   "failed to extract request ID",
			Cause:     err,
		}
	}

	// 블록체인에서 실제 문서 조회
	doc, err := ep.fetchRequestDoc(ctx, requestID)
	if err != nil {
		return nil, &EventParseError{
			EventType: "update",
			Query:     event.Query,
			Message:   "failed to fetch request document",
			Cause:     err,
		}
	}

	ep.logger.Debug("update event parsed",
		"request_id", doc.RequestId,
		"nonce", doc.Nonce,
		"status", doc.Status)

	return doc, nil
}

// ParseCompleteEvent는 완료 이벤트를 파싱
func (ep *EventParser_impl) ParseCompleteEvent(event coretypes.ResultEvent) (*CompleteEventData, error) {
	if event.Query != commontypes.CompleteQuery {
		return nil, &EventParseError{
			EventType: "complete",
			Query:     event.Query,
			Message:   "not a complete event",
		}
	}

	// 이벤트 데이터에서 필요한 정보 추출
	requestIDs, ok := event.Events[commontypes.CompleteID]
	if !ok || len(requestIDs) == 0 {
		return nil, &EventParseError{
			EventType: "complete",
			Query:     event.Query,
			Message:   "missing request ID in complete event",
		}
	}

	nonceStrs, ok := event.Events[commontypes.CompleteNonce]
	if !ok || len(nonceStrs) == 0 {
		return nil, &EventParseError{
			EventType: "complete",
			Query:     event.Query,
			Message:   "missing nonce in complete event",
		}
	}

	timestampStrs, ok := event.Events[commontypes.CompleteTime]
	if !ok || len(timestampStrs) == 0 {
		return nil, &EventParseError{
			EventType: "complete",
			Query:     event.Query,
			Message:   "missing timestamp in complete event",
		}
	}

	// 첫 번째 항목 처리 (여러 개가 있을 수 있지만 하나씩 처리)
	requestID := requestIDs[0]

	nonce, err := strconv.ParseUint(nonceStrs[0], 10, 64)
	if err != nil {
		return nil, &EventParseError{
			EventType: "complete",
			Query:     event.Query,
			Message:   "invalid nonce format",
			Cause:     err,
		}
	}

	timestamp, err := strconv.ParseUint(timestampStrs[0], 10, 64)
	if err != nil {
		return nil, &EventParseError{
			EventType: "complete",
			Query:     event.Query,
			Message:   "invalid timestamp format",
			Cause:     err,
		}
	}

	completeData := &CompleteEventData{
		RequestID: requestID,
		Nonce:     nonce,
		Timestamp: timestamp,
		BlockTime: time.Unix(int64(timestamp), 0),
	}

	ep.logger.Debug("complete event parsed",
		"request_id", requestID,
		"nonce", nonce,
		"timestamp", timestamp)

	return completeData, nil
}

// IsEventSupported는 이벤트가 지원되는지 확인
func (ep *EventParser_impl) IsEventSupported(event coretypes.ResultEvent) bool {
	switch event.Query {
	case commontypes.RegisterQuery, commontypes.UpdateQuery, commontypes.CompleteQuery:
		return true
	default:
		return false
	}
}

// extractRequestID는 이벤트에서 Request ID를 추출
func (ep *EventParser_impl) extractRequestID(event coretypes.ResultEvent, eventType string) (uint64, error) {
	requestIDStrs, ok := event.Events[eventType]
	if !ok || len(requestIDStrs) == 0 {
		return 0, fmt.Errorf("event '%s' missing request ID", eventType)
	}

	requestID, err := strconv.ParseUint(requestIDStrs[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid request ID format in event '%s': %w", eventType, err)
	}

	return requestID, nil
}

// fetchRequestDoc는 블록체인에서 요청 문서를 조회
func (ep *EventParser_impl) fetchRequestDoc(ctx context.Context, requestID uint64) (*oracletypes.OracleRequestDoc, error) {
	req := &oracletypes.QueryOracleRequestDocRequest{
		RequestId: requestID,
	}

	resp, err := ep.queryClient.OracleRequestDoc(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query request document: %w", err)
	}

	if resp.RequestDoc.RequestId == 0 {
		return nil, fmt.Errorf("request document not found for ID %d", requestID)
	}

	return &resp.RequestDoc, nil
}

// ConvertToJob는 요청 문서를 Oracle 작업으로 변환
func (ep *EventParser_impl) ConvertToJob(doc *oracletypes.OracleRequestDoc, assignedAddress string, blockTime time.Time) (*OracleJob, error) {
	if doc == nil {
		return nil, &EventParseError{
			EventType: "conversion",
			Message:   "request document cannot be nil",
		}
	}

	// 이 노드가 처리해야 하는지 확인
	assignedIndex, isAssigned := ep.findAssignedIndex(doc.AccountList, assignedAddress)
	if !isAssigned {
		return nil, &EventParseError{
			EventType: "conversion",
			Message:   "request not assigned to this node",
		}
	}

	// 담당 엔드포인트 선택
	if len(doc.Endpoints) == 0 {
		return nil, &EventParseError{
			EventType: "conversion",
			Message:   "no endpoints available",
		}
	}

	endpointIndex := assignedIndex % len(doc.Endpoints)
	endpoint := doc.Endpoints[endpointIndex]

	// 다음 실행 시간 계산
	nextRunTime := ep.calculateNextRunTime(doc, blockTime)

	// 작업 ID 생성
	jobID := fmt.Sprintf("job_%d_%d", doc.RequestId, doc.Nonce)

	job := &OracleJob{
		ID:            jobID,
		RequestID:     doc.RequestId,
		URL:           endpoint.Url,
		ParseRule:     endpoint.ParseRule,
		Nonce:         doc.Nonce,
		NextRunTime:   nextRunTime,
		Period:        time.Duration(doc.Period) * time.Second,
		Status:        doc.Status,
		AccountList:   doc.AccountList,
		AssignedIndex: assignedIndex,

		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		RunCount:     0,
		FailureCount: 0,

		ExecutionState: commontypes.ExecutionState{
			Status:        commontypes.JobStatusPending,
			LastHeartbeat: &[]time.Time{time.Now()}[0],
		},
		Priority:      ep.calculateJobPriority(doc),
		RetryAttempts: 0,
		MaxRetries:    3, // 기본값
	}

	ep.logger.Debug("job converted from request doc",
		"job_id", job.ID,
		"request_id", job.RequestID,
		"url", job.URL,
		"next_run", job.NextRunTime)

	return job, nil
}

// findAssignedIndex는 계정 목록에서 할당된 인덱스를 찾음
func (ep *EventParser_impl) findAssignedIndex(accountList []string, address string) (int, bool) {
	for i, account := range accountList {
		if account == address {
			return i, true
		}
	}
	return -1, false
}

// calculateNextRunTime은 다음 실행 시간을 계산
func (ep *EventParser_impl) calculateNextRunTime(doc *oracletypes.OracleRequestDoc, blockTime time.Time) time.Time {
	period := time.Duration(doc.Period) * time.Second
	now := time.Now()

	// 블록 시간 기준으로 다음 실행 시간 계산
	nextRun := blockTime.Add(period)

	// 현재 시간보다 과거이면 즉시 실행
	if nextRun.Before(now) {
		return now
	}

	return nextRun
}

// calculateJobPriority는 작업 우선순위를 계산
func (ep *EventParser_impl) calculateJobPriority(doc *oracletypes.OracleRequestDoc) int {
	// 기본 우선순위는 중간 (5)
	priority := 5

	// Nonce가 0이면 높은 우선순위 (새로운 요청)
	if doc.Nonce == 0 {
		priority += 3
	}

	// Period가 짧을수록 높은 우선순위
	if doc.Period <= 30 { // 30초 이하
		priority += 2
	} else if doc.Period <= 300 { // 5분 이하
		priority += 1
	}

	return priority
}

// BatchParseCompleteEvents는 여러 완료 이벤트를 일괄 파싱
func (ep *EventParser_impl) BatchParseCompleteEvents(events []coretypes.ResultEvent) ([]*CompleteEventData, []error) {
	var results []*CompleteEventData
	var errors []error

	for _, event := range events {
		if event.Query == commontypes.CompleteQuery {
			data, err := ep.ParseCompleteEvent(event)
			if err != nil {
				errors = append(errors, err)
				continue
			}
			results = append(results, data)
		}
	}

	return results, errors
}

// 에러 타입 정의

// EventParseError는 이벤트 파싱 에러를 나타내는 구조체
type EventParseError struct {
	EventType string // 이벤트 타입 (register, update, complete)
	Query     string // 이벤트 쿼리
	Message   string // 에러 메시지
	Cause     error  // 원인 에러
}

// Error는 error 인터페이스 구현
func (e *EventParseError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("event parse error [%s] for query '%s': %s (cause: %v)",
			e.EventType, e.Query, e.Message, e.Cause)
	}
	return fmt.Sprintf("event parse error [%s] for query '%s': %s",
		e.EventType, e.Query, e.Message)
}

// Unwrap은 원인 에러를 반환
func (e *EventParseError) Unwrap() error {
	return e.Cause
}

// IsRetryable은 에러가 재시도 가능한지 확인
func (e *EventParseError) IsRetryable() bool {
	// 네트워크 에러나 일시적 에러인 경우 재시도 가능
	switch e.EventType {
	case "register", "update":
		// 블록체인 쿼리 실패는 재시도 가능
		return true
	case "complete":
		// 완료 이벤트 파싱 실패는 보통 재시도해도 소용없음
		return false
	default:
		return false
	}
}

// ValidationError는 데이터 검증 에러를 나타내는 구조체
type ValidationError struct {
	Field   string // 검증 실패한 필드
	Value   string // 검증 실패한 값
	Message string // 에러 메시지
}

// Error는 error 인터페이스 구현
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s' with value '%s': %s",
		e.Field, e.Value, e.Message)
}

// NewValidationError는 새로운 검증 에러를 생성
func NewValidationError(field, value, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}
