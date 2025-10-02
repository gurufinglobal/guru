# Oracle Daemon 처리 흐름 상세 문서

## 전체 아키텍처

```
┌─────────────────────────────────────────────────────────────────────┐
│                       Oracle Daemon 처리 흐름                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. Daemon 시작                                                      │
│     ├── Config 로드 (/home/user/.oracled/config.toml)              │
│     ├── Keyring 초기화                                              │
│     ├── CometBFT Client 연결                                        │
│     ├── Components 초기화                                           │
│     │   ├── EventWatcher                                            │
│     │   ├── EventScheduler (+ JobStore, JobExecutor)               │
│     │   └── ResultSubmitter (+ TxBuilder, SequenceManager)         │
│     └── Background 고루틴 시작                                       │
│         ├── runEventProcessor                                       │
│         ├── runResultProcessor                                      │
│         ├── runHealthMonitor                                        │
│         └── runMetricsCollector                                     │
│                                                                     │
│  2. 이벤트 구독 (Watcher)                                            │
│     ├── Register Query 구독                                         │
│     ├── Update Query 구독                                           │
│     └── Complete Query 구독                                         │
│                                                                     │
│  3. 기존 작업 로드 (Scheduler)                                       │
│     └── QueryClient를 통해 활성 Oracle 요청 조회 및 JobStore 저장    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## 이벤트 처리 흐름

### 1. Register Event (새 Oracle 요청 등록)

```
블록체인에서 MsgRegisterOracleRequestDoc 감지
    ↓
Watcher: 이벤트 수신 → EventCh로 전달
    ↓
Daemon: runEventProcessor → Scheduler.ProcessEvent() 호출
    ↓
Scheduler: 
    ├── EventParser.ParseRegisterEvent() 
    │   ├── 이벤트에서 RequestID 추출
    │   ├── QueryClient로 OracleRequestDoc 조회
    │   └── ConvertToJob() - Job 객체 생성
    ├── JobStore.Store("req_{RequestID}", job)
    │   └── NextRunTime = now (즉시 실행 가능)
    └── Metrics 업데이트
```

**Job 초기 상태:**
- `ExecutionState.Status`: `JobStatusPending`
- `NextRunTime`: `time.Now()` (즉시 실행)
- `Nonce`: 문서의 현재 nonce
- `Period`: 문서의 period

### 2. Update Event (Oracle 요청 수정)

```
블록체인에서 MsgUpdateOracleRequestDoc 감지
    ↓
Watcher: 이벤트 수신 → EventCh로 전달
    ↓
Daemon: runEventProcessor → Scheduler.ProcessEvent() 호출
    ↓
Scheduler:
    ├── EventParser.ParseUpdateEvent()
    ├── ConvertToJob() - 새 Job 객체 생성
    └── JobStore.Update("req_{RequestID}")
        ├── URL 업데이트
        ├── ParseRule 업데이트
        ├── Period 업데이트
        ├── Status 업데이트
        └── AccountList 업데이트
```

**업데이트되는 필드:**
- URL, ParseRule, Period, Status, AccountList, AssignedIndex
- Nonce는 Complete 이벤트에서만 업데이트

### 3. 주기적 Job 실행

```
매 1초마다 scheduleTicker 트리거
    ↓
Scheduler: checkAndExecuteReadyJobs()
    ├── JobStore.GetReadyJobs(now) - 실행 준비된 작업 조회
    │   └── 조건: Status=ENABLED, ExecutionState!=EXECUTING/COMPLETED, 
    │             NextRunTime <= now
    ├── TaskGroup에 executeJobToResult() 제출
    └── 각 Job 비동기 실행
        ↓
executeJobToResult():
    ├── 1. Job 상태 → JobStatusExecuting
    ├── 2. JobExecutor.FetchData(job.URL)
    │   ├── HTTP GET 요청 (최대 3회 재시도)
    │   └── 실패 시: Status=Failed, NextRunTime=now+1분
    ├── 3. JobExecutor.ParseAndExtract(rawData, job.ParseRule)
    │   ├── JSON 파싱
    │   ├── 경로 탐색 (dot notation)
    │   └── 실패 시: Status=Failed, NextRunTime=now+5분
    └── 4. 성공 시:
        ├── Job 상태 → JobStatusCompleted
        ├── NextRunTime → 24시간 후 (Complete 대기)
        ├── JobResult 생성 (Nonce: job.Nonce + 1)
        └── ResultCh로 전송
```

**실행 조건 (isJobReady):**
- `job.Status == REQUEST_STATUS_ENABLED`
- `job.ExecutionState.Status ∉ {Executing, Retrying, Completed}`
- `job.NextRunTime <= now`

### 4. 결과 제출

```
Scheduler: JobResult → ResultCh
    ↓
Daemon: runResultProcessor
    ├── ResultCh에서 수신
    └── ResultSubmitter.Submit() 호출
        ↓
Submitter: submitWithRetry()
    ├── TxBuilder.BuildSubmitTx()
    │   ├── MsgSubmitOracleData 생성
    │   │   └── DataSet { RequestID, RawData, Nonce, Provider }
    │   ├── TxFactory 생성 (sequence, gas, fees)
    │   ├── 트랜잭션 서명
    │   └── 트랜잭션 인코딩
    ├── ClientCtx.BroadcastTx() - 블록체인 제출
    ├── 재시도 로직 (최대 5회)
    │   └── Sequence 에러 시 자동 동기화
    └── 성공 시:
        ├── SequenceManager.NextSequence()
        └── Metrics 업데이트
```

**트랜잭션 내용:**
- Type: `MsgSubmitOracleData`
- RequestID: Job의 RequestID
- Nonce: `job.Nonce + 1` (다음 nonce)
- RawData: 추출된 데이터
- Provider: Daemon의 주소

### 5. Complete Event (데이터 집계 완료)

```
블록체인에서 Complete 이벤트 발생 (Quorum 달성 후)
    ↓
Watcher: 이벤트 수신 → EventCh로 전달
    ↓
Daemon: runEventProcessor → Scheduler.ProcessEvent() 호출
    ↓
Scheduler: handleCompleteEvent()
    ├── EventParser.ParseCompleteEvent()
    │   ├── RequestID 추출 및 파싱 (uint64)
    │   ├── Nonce 추출
    │   └── BlockTime 추출
    └── JobStore.Update("req_{RequestID}")
        ├── Nonce = completeData.Nonce (완료된 nonce)
        ├── NextRunTime = BlockTime + Period
        ├── ExecutionState.Status = JobStatusPending
        └── 다음 실행 준비 완료
```

**다음 실행 시간 계산:**
- `NextRunTime = CompleteEvent.BlockTime + job.Period`
- 과거 시간인 경우: `NextRunTime = now` (즉시 실행)

## 상태 전이도

```
Job Lifecycle:

[새 등록] → PENDING
               ↓ (NextRunTime <= now)
          EXECUTING
               ↓
         성공 / 실패
        /           \
   COMPLETED      FAILED
   (Complete       (1~5분 후
    이벤트 대기)    재시도)
        ↓             ↓
   [Complete]    PENDING
        ↓
   PENDING
   (다음 주기)
```

## Nonce 관리

```
Initial State:
    OracleRequestDoc.Nonce = 0
    Job.Nonce = 0

실행 흐름:
    1. Job 실행 → 데이터 수집
    2. JobResult 생성 (Nonce = 0 + 1 = 1)
    3. 트랜잭션 제출 (Nonce = 1)
    4. Complete 이벤트 수신 (Nonce = 1)
    5. Job.Nonce 업데이트 = 1
    6. 다음 실행 → Nonce = 1 + 1 = 2

핵심:
    - Job.Nonce: 마지막으로 완료된 nonce
    - 제출 시: Job.Nonce + 1
    - Complete 후: Job.Nonce = 완료된 nonce
```

## 에러 처리 및 재시도

### Job 실행 실패

| 실패 단계 | NextRunTime | 재시도 간격 | 이유 |
|-----------|-------------|-------------|------|
| Fetch 실패 | now + 1분 | 1분 | 네트워크 오류는 빠른 복구 가능 |
| Parse 실패 | now + 5분 | 5분 | 구조 문제는 빠른 재시도 무의미 |

### 트랜잭션 제출 실패

| 에러 코드 | 에러 타입 | 처리 방법 |
|-----------|-----------|-----------|
| 32 | Sequence Mismatch | Chain과 동기화 후 재시도 |
| 19 | TX in Mempool | Sequence만 증가 |
| 13 | Insufficient Fee | 로깅 (설정에서 gas 조정 필요) |
| 11 | Out of Gas | 가스 조정 후 재시도 |

**재시도 전략:**
- 최대 재시도: 5회
- 초기 지연: 1초
- 최대 지연: 30초
- 증가 방식: 지수 백오프 (2배씩)
- Jitter: 10% 랜덤

## 안전한 종료 흐름

```
Context 취소 또는 Stop() 호출
    ↓
1. isRunning = false
2. shutdownCh 닫기
    ↓
3. Components 순차 종료:
    ├── EventWatcher.Stop()
    │   ├── Subscriptions 취소
    │   ├── EventCh 닫기
    │   └── ErrorCh 닫기
    ├── JobScheduler.Stop()
    │   ├── scheduleTicker 정지
    │   ├── scheduleWg.Wait() (주기적 스케줄러 대기)
    │   ├── JobExecutor.Shutdown() (TaskGroup 대기)
    │   └── ResultCh 닫기
    └── ResultSubmitter.Stop()
        ├── metricsTicker, syncTicker 정지
        ├── 진행 중인 제출 완료 대기
        └── submitSemaphore 닫기
    ↓
4. CometBFT Client 종료
    ↓
5. wg.Wait() - 모든 고루틴 종료 대기
    ↓
6. fatalCh 닫기
    ↓
7. 완료
```

**채널 닫기 순서:**
1. `shutdownCh` (daemon)
2. `EventCh`, `ErrorCh` (watcher)
3. `ResultCh` (scheduler)
4. `submitSemaphore` (submitter)
5. `fatalCh` (daemon)

## 동시성 제어

### TaskGroup (JobExecutor)
- **용도**: Job 비동기 실행
- **Limit**: `config.WorkerPoolSize` (기본 4)
- **기능**: CPU 기반 동시성 제어

### Semaphore (Submitter)
- **용도**: 트랜잭션 제출 동시성 제한
- **Capacity**: `config.WorkerPoolSize` (기본 4)
- **기능**: 과도한 제출 방지

### Mutex 사용
- **JobStore**: `sync.RWMutex` (읽기/쓰기 분리)
- **SequenceManager**: `sync.RWMutex` (sequence 동기화)
- **Metrics**: `sync.RWMutex` (메트릭 업데이트)

## 핵심 설계 원칙

### 1. 중복 실행 방지
- 성공 후 `Status = Completed`, `NextRunTime = 24시간 후`
- Complete 이벤트 전까지 재실행 불가
- Complete 후에만 `Status = Pending`, NextRunTime 재계산

### 2. 안전한 재시도
- Fetch 실패: 1분 후 재시도
- Parse 실패: 5분 후 재시도 (데이터 구조 문제)
- 재시도 시에도 Nonce는 변경 안 됨

### 3. Nonce 동기화
- Job.Nonce: 마지막 완료된 nonce
- 제출 시: Job.Nonce + 1
- Complete 후: Job.Nonce 업데이트

### 4. Sequence 관리
- 초기 동기화: Daemon 시작 시
- 자동 동기화: 5분마다
- 에러 복구: Sequence 에러 시 즉시 동기화
- 원자적 증가: 제출 성공 시에만

## 메트릭 및 모니터링

### Daemon Metrics
- `EventsProcessed`: 처리된 이벤트 수
- `JobsCompleted`: 완료된 작업 수
- `TxSubmitted`: 제출된 트랜잭션 수
- `TotalErrors`: 총 에러 수
- `HealthStatus`: 헬스 상태

### Health Check (30초마다)
- CometBFT 클라이언트 연결 상태
- Watcher 구독 활성 상태
- Scheduler 실행 상태
- Submitter 실행 상태
- 최근 이벤트 수신 여부 (5분 이내)

## 설정 파일 (~/.oracled/config.toml)

```toml
[chain]
id = 'guru_631-1'
endpoint = 'http://localhost:26657'

[key]
name = 'mykey'
keyring_dir = '/home/user/.oracled'
keyring_backend = 'test'

[gas]
limit = 70000
adjustment = 1.5
prices = '630000000000'

[retry]
max_attempts = 4
max_delay_sec = 10

[worker]
pool_size = 4          # TaskGroup 동시 실행 수
channel_size = 1024    # Event channel 버퍼
timeout_sec = 30       # HTTP 요청 타임아웃
```

## 운영 가이드

### 정상 시작
```bash
./oracled
# 또는
./oracled --home /custom/path
```

### 로그 확인
```bash
tail -f ~/.oracled/logs/oracled.*.log
```

### 정상 종료
- `Ctrl+C` (SIGINT)
- `SIGTERM` 신호
→ 진행 중인 작업 완료 후 안전하게 종료

### 비정상 종료 복구
1. Daemon 재시작
2. `loadExistingJobs()` 자동 실행
3. 활성 Oracle 요청들 자동 로드
4. 정상 운영 재개

## 주요 개선 사항 (v2)

1. **TaskGroup 기반 실행**: Worker pool 대신 creachadair/taskgroup 사용
2. **안전한 Nonce 관리**: Complete 이벤트 기반 동기화
3. **중복 실행 방지**: JobStatusCompleted 상태로 재실행 차단
4. **안전한 종료**: WaitGroup과 채널 순차 정리
5. **에러 복구**: 실패 시 자동 재시도 (지수 백오프)
6. **Sequence 동기화**: 자동 복구 및 주기적 동기화
7. **Update 이벤트**: 모든 필드 동적 업데이트 지원

