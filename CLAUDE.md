You are working on the gurufinglobal/guru blockchain codebase.
Working directory: /Users/jarvis/.openclaw/workspace/gurufin/guru
Branch: feat/upgrade-cosmos-evm (already created, based on dev)

## Context
This is a Cosmos SDK EVM chain. The codebase has "internalized" cosmos/evm — meaning x/vm, x/erc20, x/feemarket, x/precisebank, precompiles/, ante/evm/, rpc/, etc. are all copied directly from cosmos/evm and live inside this repo. There is NO external cosmos/evm dependency in go.mod.

Your task: **Track A — In-place upgrade** of the internalized cosmos/evm code from v0.3.1 → v0.5.1.

## STEP 1: Analysis & Planning (do this first)
1. Run `go build ./...` to confirm current state compiles
2. Compare cosmos/evm v0.3.1 vs v0.5.1 for the following directories by fetching from GitHub:
   - x/vm/keeper/
   - x/vm/types/
   - x/feemarket/
   - ante/evm/
   - rpc/
   - precompiles/
3. Identify all differences relevant to guru's codebase
4. Write a plan to /Users/jarvis/.openclaw/workspace/gurufin/track-a-plan.md

## STEP 2: Phase 1 — Dependency updates
1. Update go.mod:
   - go-ethereum: cosmos/go-ethereum v1.15.11-cosmos-0 (already correct, verify)
   - ibc-go: already v10.3.0 (verify)
   - cometbft: already v0.38.19 (verify)
2. Run `go mod tidy`
3. Run `go build ./...` — fix any compilation errors
4. Commit: "chore(deps): verify and align dependencies with cosmos/evm v0.5.1"

## STEP 3: Phase 2 — x/vm core updates
Apply changes from cosmos/evm v0.5.1 to guru's x/vm/:
Key changes to apply:
- Remove EvmAppOptions pattern → move EVM coin info to genesis (#661)
  - guru uses EvmAppOptions in gurud/config.go and cmd/gurud/cmd/root.go
  - New pattern: EVMCoinInfo set in genesis state, not via options function
- Remove x/params usage (#594)
- Apply mempool race condition fixes (#656, #658)
- Apply non-deterministic state mutation fix (#729)
- Apply InitEvmCoinInfo upgrade handler (#736)
- Apply EVM events → txResult.Data change (#576)
- Apply mempool improvements (#467, #496, #538, #568, #582, #598, #630)
- Precompile constructor interface refactor (#477, #577)
  - NewPrecompile now accepts keeper interfaces, not concrete types
  - NewPrecompile no longer returns error

CRITICAL: guru has custom modules (x/bex, x/oracle, x/feepolicy) — preserve these completely.
CRITICAL: gurufinglobal/cosmos-sdk fork replace directives must be preserved.
CRITICAL: After each sub-step, run `go build ./...` to verify compilation.

## STEP 4: Phase 3 — Precompiles update
Apply precompile changes from cosmos/evm v0.5.1:
- Add precompiles/callbacks/ (new in v0.5.1)
- Add precompiles/types/ (new in v0.5.1)  
- Remove precompiles/evidence/ (removed in v0.5.1)
- Update all existing precompile constructors to new interface (#477)
- Apply StaticPrecompiles builder pattern (#680)
- Update gurud/precompiles.go and gurud/activators.go

## STEP 5: Phase 4 — JSON-RPC & RPC updates
Apply RPC changes:
- Add debug_traceCall (#711)
- Add eth_createAccessList (#346)
- Add state overrides in eth_call (#337)
- Fix CometBlockResultByNumber height=0 (#416)
- Fix inconsistent block hash (#725)
- Fill block hash and timestamp (#584)

## STEP 6: Phase 5 — local_node.sh verification
1. Run `go build ./...` — must pass cleanly
2. Run `./local_node.sh -y` and verify node starts
3. Wait 30 seconds, check if blocks are being produced: `curl -s localhost:26657/status | jq .result.sync_info`
4. If successful, commit: "feat: upgrade cosmos/evm internalized code to v0.5.1"

## Rules
- After EVERY phase, run `go build ./...` and fix errors before moving on
- Commit after each completed phase with descriptive message
- If you encounter a breaking change that requires a decision, write it to /Users/jarvis/.openclaw/workspace/gurufin/track-a-decisions.md and continue with the best available option
- Do NOT modify go.mod replace directives for gurufinglobal/cosmos-sdk
- Do NOT touch x/bex, x/oracle, x/feepolicy modules
- Use `git diff --stat HEAD` to summarize changes after each phase

When completely finished with all phases, run:
openclaw system event --text "Track A complete: cosmos/evm v0.5.1 in-place upgrade done. Check track-a-plan.md for details." --mode now
