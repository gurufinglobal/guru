# Guru Chain 네트워크 런칭 후 체크리스트

## 1. 기본 네트워크 상태 확인 (Basic Network Health)

### 1.1 노드 상태 확인
- [ ] **노드 실행 상태 확인**
  ```bash
  ps -ef | grep gurud | grep -v grep
  systemctl status gurud  # systemd 사용 시
  ```

- [ ] **블록 생성 확인**
  ```bash
  gurud status | jq .SyncInfo
  # latest_block_height가 지속적으로 증가하는지 확인
  ```

- [ ] **P2P 연결 상태 확인**
  ```bash
  gurud status | jq .SyncInfo.catching_up
  # false여야 정상 (동기화 완료)
  curl localhost:26657/net_info | jq .result.n_peers
  # 연결된 피어 수 확인
  ```

### 1.2 합의 상태 확인
- [ ] **밸리데이터 상태 확인**
  ```bash
  gurud query staking validators --output json | jq '.validators[] | {moniker, status, tokens, jailed}'
  ```

- [ ] **블록 시간 확인**
  ```bash
  # 블록 시간이 설정값(기본 ~6초) 내에서 생성되는지 확인
  gurud query block | jq .block.header.time
  ```

- [ ] **미스드 블록 확인**
  ```bash
  gurud query slashing signing-info $(gurud tendermint show-validator)
  ```

## 2. EVM 호환성 확인 (EVM Compatibility)

### 2.1 JSON-RPC 서비스 확인
- [ ] **JSON-RPC 엔드포인트 응답 확인**
  ```bash
  curl -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
    http://localhost:8545
  # Chain ID: 631 (0x277) 확인
  ```

- [ ] **Web3 호환성 확인**
  ```bash
  curl -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}' \
    http://localhost:8545
  ```

- [ ] **최신 블록 정보 확인**
  ```bash
  curl -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
    http://localhost:8545
  ```

### 2.2 EVM 상태 확인
- [ ] **EVM 파라미터 확인**
  ```bash
  gurud query evm params
  # evm_denom, enable_create, enable_call 등 확인
  ```

- [ ] **EVM 계정 확인**
  ```bash
  gurud query evm account <eth_address>
  ```

## 3. 커스텀 모듈 확인 (Custom Modules)

### 3.1 ERC20 모듈
- [ ] **토큰 페어 확인**
  ```bash
  gurud query erc20 token-pairs
  ```

- [ ] **네이티브 프리컴파일 확인**
  ```bash
  gurud query erc20 params
  # native_precompiles 리스트 확인
  ```

### 3.2 Fee Market 모듈
- [ ] **베이스 수수료 확인**
  ```bash
  gurud query feemarket base-fee
  gurud query feemarket params
  ```

### 3.3 Oracle 모듈
- [ ] **오라클 상태 확인**
  ```bash
  gurud query oracle params
  gurud query oracle aggregates
  ```

### 3.4 Precise Bank 모듈
- [ ] **정밀 잔액 확인**
  ```bash
  gurud query precisebank total-supply
  gurud query precisebank fractional-balance <address>
  ```

### 3.5 Fee Policy 모듈
- [ ] **수수료 정책 확인**
  ```bash
  gurud query feepolicy params
  ```

## 4. 프리컴파일 컨트랙트 확인 (Precompiled Contracts)

### 4.1 정적 프리컴파일 테스트
- [ ] **Staking 프리컴파일 (0x800)**
  ```javascript
  // Web3.js 또는 ethers.js로 테스트
  const stakingContract = new web3.eth.Contract(stakingABI, '0x0000000000000000000000000000000000000800');
  ```

- [ ] **Distribution 프리컴파일 (0x801)**
- [ ] **ICS20 프리컴파일 (0x802)**
- [ ] **Bank 프리컴파일 (0x804)**
- [ ] **Governance 프리컴파일 (0x805)**
- [ ] **Slashing 프리컴파일 (0x806)**
- [ ] **Evidence 프리컴파일 (0x807)**

### 4.2 유틸리티 프리컴파일 테스트
- [ ] **P256 프리컴파일 (0x100)**
- [ ] **Bech32 프리컴파일 (0x400)**

## 5. 네트워크 성능 및 모니터링 (Performance & Monitoring)

### 5.1 메트릭스 확인
- [ ] **Prometheus 메트릭스 활성화**
  ```bash
  curl http://localhost:26660/metrics | grep tendermint
  ```

- [ ] **EVM JSON-RPC 메트릭스 확인**
  ```bash
  curl http://localhost:6065/debug/metrics/prometheus
  ```

### 5.2 로그 확인
- [ ] **노드 로그 확인**
  ```bash
  tail -f ~/.gurud/logs/gurud.log
  # 또는 journalctl -fu gurud
  ```

- [ ] **에러 로그 모니터링**
  ```bash
  grep -i error ~/.gurud/logs/gurud.log
  ```

### 5.3 성능 지표 확인
- [ ] **TPS (초당 트랜잭션) 측정**
- [ ] **블록 시간 일관성 확인**
- [ ] **메모리 사용량 모니터링**
- [ ] **디스크 사용량 확인**

## 6. 거버넌스 및 업그레이드 (Governance & Upgrades)

### 6.1 거버넌스 기능 확인
- [ ] **거버넌스 파라미터 확인**
  ```bash
  gurud query gov params
  ```

- [ ] **프로포절 생성/투표 테스트**
  ```bash
  # 테스트넷에서만 수행
  gurud tx gov submit-proposal --help
  ```

### 6.2 업그레이드 준비
- [ ] **업그레이드 핸들러 확인**
- [ ] **마이그레이션 스크립트 준비**
- [ ] **백업 절차 확인**

## 7. 보안 검증 (Security Validation)

### 7.1 키 관리
- [ ] **밸리데이터 키 보안 확인**
- [ ] **노드 키 권한 확인**
  ```bash
  ls -la ~/.gurud/config/
  # priv_validator_key.json, node_key.json 권한 확인
  ```

### 7.2 네트워크 보안
- [ ] **방화벽 설정 확인**
- [ ] **RPC 엔드포인트 접근 제한**
- [ ] **민감한 API 비활성화 확인**

### 7.3 트랜잭션 보안
- [ ] **가스 한도 설정 확인**
- [ ] **수수료 정책 확인**
- [ ] **스팸 방지 메커니즘 확인**

## 8. 외부 연동 확인 (External Integrations)

### 8.1 IBC 연결
- [ ] **IBC 채널 상태 확인**
  ```bash
  gurud query ibc channel channels
  ```

- [ ] **IBC 전송 테스트**

### 8.2 오라클 데이터
- [ ] **외부 데이터 피드 확인**
- [ ] **오라클 데몬 상태 확인**
  ```bash
  ps -ef | grep oracled
  ```

## 9. 백업 및 복구 (Backup & Recovery)

### 9.1 데이터 백업
- [ ] **제네시스 파일 백업**
- [ ] **설정 파일 백업**
- [ ] **밸리데이터 키 백업**
- [ ] **체인 데이터 스냅샷**

### 9.2 복구 절차 테스트
- [ ] **노드 재시작 테스트**
- [ ] **데이터 복구 테스트**
- [ ] **네트워크 재참여 테스트**

## 10. 문서화 및 운영 (Documentation & Operations)

### 10.1 운영 문서
- [ ] **네트워크 파라미터 문서화**
- [ ] **긴급 상황 대응 절차**
- [ ] **모니터링 대시보드 설정**

### 10.2 커뮤니티 지원
- [ ] **엔드포인트 정보 공개**
- [ ] **개발자 가이드 업데이트**
- [ ] **FAQ 및 트러블슈팅 가이드**

## 체크리스트 사용법

1. **단계별 진행**: 각 섹션을 순서대로 진행하되, 중요도에 따라 우선순위를 조정할 수 있습니다.
2. **자동화 스크립트**: 반복적인 체크는 스크립트로 자동화하여 효율성을 높입니다.
3. **정기 점검**: 런칭 후에도 정기적으로 체크리스트를 활용하여 네트워크 상태를 점검합니다.
4. **문제 발생 시**: 각 항목에서 문제가 발견되면 즉시 해당 로그를 확인하고 문제를 해결합니다.

## 주요 설정 파일 위치

- **설정 파일**: `~/.gurud/config/config.toml`
- **앱 설정**: `~/.gurud/config/app.toml`  
- **제네시스**: `~/.gurud/config/genesis.json`
- **로그**: `~/.gurud/logs/` (설정에 따라 다름)

## 기본 포트 정보

- **CometBFT RPC**: 26657
- **CometBFT P2P**: 26656  
- **Cosmos REST API**: 1317
- **Cosmos gRPC**: 9090
- **EVM JSON-RPC**: 8545
- **EVM WebSocket**: 8546
- **Prometheus 메트릭스**: 26660
- **EVM 메트릭스**: 6065
