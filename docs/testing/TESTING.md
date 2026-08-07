# Testing Architecture

## Test tiers

- **unit**  
  Deterministic, in-memory tests for single keeper/module helpers and pure logic.
- **integration**  
  Compatibility alias currently mapped to the same test scope as unit tests (module/app packages).
- **e2e**  
  Tests that execute `gurud` runtime, local nodes, RPC/gRPC, and on-chain runtime flows.
- **policy**  
  Repository and source-policy checks (imports, module boundaries, versioning contracts).
- **two-chain**  
  Multi-node / multi-chain compatibility scenarios under `tests/transwaptwochain`.
- **oracle**  
  Standalone Oracle module unit/integration tests plus legacy root oracle package tests.
- **oracle-soak**  
  Time-based or long-running Oracle / consensus stress tests.
- **race**  
  Full race-detector runs.
- **cover**  
  Coverage-enabled runs for unit/integration-oracle baseline packages.

## Makefile mapping

- `make test`
  - Runs unit suite only (delegates to `test-unit`).
- `make test-unit`
  - `go test -mod=readonly ./app/... ./x/...`
- `make test-integration`
  - Alias of `make test-unit` for backward compatibility.
- `make test-e2e`
  - `go test -mod=readonly -tags=e2e ./tests/pulsarcompat`
- `make test-policy`
  - `go test -mod=readonly -tags=policy -run 'TestPolicy' ./tests/pulsarcompat`
- `make test-twochain`
  - `go test -mod=readonly ./tests/transwaptwochain`
- `make test-oracle`
  - `go test -mod=readonly ./x/oracle`
  - `GOWORK=off go -C oracle test -mod=readonly ./...`
- `make test-oracle-soak`
  - `go test -mod=readonly -tags=soak -run 'Soak' ./tests/pulsarcompat`
- `make test-race`
  - `go test -mod=readonly -race ./app/... ./x/...`
  - `GOWORK=off go -C oracle test -mod=readonly -race ./...`
- `make test-cover`
  - `go test -mod=readonly -cover ./app/... ./x/...`
  - `GOWORK=off go -C oracle test -mod=readonly -cover ./...`
- `make test-ci`
  - `test-unit`
  - `test-integration`
  - `test-policy`
  - `test-oracle`
- `make test-all`
  - `test-unit`
  - `test-integration`
  - `test-policy`
  - `test-e2e`
  - `test-twochain`

## Build tags

- `e2e`: Tests that execute binaries, nodes, or runtime paths.
- `policy`: Repository policy checks and source boundary assertions.
- `soak`: Long-running or time-based stress tests.

## Runtime and execution notes

- Default `make test` does not execute:
  - `gurud` binary node startup
  - real multi-node flows
  - soak suites
  - policy checks
- E2E requires prerequisite setup (binary build, available test ports, runtime environment).
- Policy and soak suites are separated from the default path and should be run explicitly with dedicated targets.

## Ownership guideline

- Each invariant should be owned by one principal layer whenever possible.
- Upper layers should validate only representative behavior (routing, rollback/rollback boundaries) and should not duplicate full matrix coverage.
- Do not weaken validation for:
  - security boundaries
  - custody guarantees
  - rollback correctness
  - duplicate settlement prevention
  - atomic execution semantics

## High-risk suites to keep

- Keeper boundary, reserve/fee custody, authz signer path, exchange lifecycle, and state consistency.
- Transwap ACK / timeout / refund / retry / duplicate settlement paths and atomicity.
- App runtime message routing, EVM–IBC boundaries, and module account permissions.
