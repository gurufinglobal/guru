You are working on a copy of the gurufinglobal/guru blockchain codebase.
Working directory: /Users/jarvis/.openclaw/workspace/gurufin/guru_ext
Branch: feat/upgrade-cosmos-evm-ext (already created, based on dev)

## Context
This is a Cosmos SDK EVM chain that currently has cosmos/evm code internalized (embedded directly).
Your task: **Track B — External dependency migration** — replace the internalized cosmos/evm code with cosmos/evm v0.5.1 as an external go.mod dependency.

## STEP 1: Analysis
1. Run `go build ./...` to confirm current state
2. List all files that are copied from cosmos/evm (x/vm/, x/erc20/, x/feemarket/, x/precisebank/, ante/evm/, rpc/, precompiles/ shared ones, crypto/ethsecp256k1/, encoding/, types/ EVM-related)
3. Write analysis to /Users/jarvis/.openclaw/workspace/gurufin/track-b-plan.md

## STEP 2: Add cosmos/evm v0.5.1 dependency
1. Edit go.mod to add: `github.com/cosmos/evm v0.5.1`
2. Add necessary replace directives if needed
3. Run `go mod tidy`

## STEP 3: Remove internalized cosmos/evm code
Gradually replace internalized code with cosmos/evm imports:

Phase 3a - Replace x/vm (EVM core module):
- Remove x/vm/ directory
- Update all imports from `github.com/gurufinglobal/guru/v2/x/vm` → `github.com/cosmos/evm/x/vm`
- Fix any compilation errors (cosmos/evm v0.5.1 API changes)
- Run `go build ./...`

Phase 3b - Replace x/feemarket, x/erc20, x/precisebank:
- Remove these directories
- Update all imports
- Run `go build ./...`

Phase 3c - Replace ante/evm/:
- cosmos/evm v0.5.1 provides evm/ante as a library
- Update gurud/ante to use cosmos/evm ante handlers
- Run `go build ./...`

Phase 3d - Replace rpc/:
- Update rpc/ to use cosmos/evm rpc package
- Run `go build ./...`

Phase 3e - Replace shared precompiles (bank, bech32, distribution, erc20, gov, ics20, p256, slashing, staking, werc20):
- Remove these precompile directories
- Update imports to use cosmos/evm precompiles
- Keep any GURU-SPECIFIC precompiles that don't exist in cosmos/evm
- Run `go build ./...`

## STEP 4: Fix Breaking Changes from v0.5.1
Apply all breaking changes:
- EvmAppOptions removal → use genesis initialization (#661)
  - Add EVMCoinInfo to genesis state in gurud/app.go
  - Remove EvmAppOptions from gurud/config.go and cmd/gurud/cmd/root.go
- x/params removal (#594) — update any remaining x/params usage
- Precompile constructor signatures (#477, #577)
- StaticPrecompiles builder (#680)
- Update gurud/precompiles.go and gurud/activators.go

## STEP 5: Preserve Guru custom modules
Verify these custom modules still work with cosmos/evm v0.5.1:
- x/bex (guru-specific DEX module)
- x/oracle (guru-specific oracle)
- x/feepolicy (guru-specific fee policy)
- Any guru-specific precompiles

Fix any interface/import issues.

## STEP 6: local_node.sh verification
1. Run `go build ./...` — must pass
2. Run `./local_node.sh -y`
3. Check blocks: `curl -s localhost:26657/status | jq .result.sync_info`
4. Commit: "feat: migrate to cosmos/evm v0.5.1 as external dependency"

## Rules
- Commit after each completed phase
- If you encounter ambiguity about which cosmos/evm files can be safely deleted, keep them and note in track-b-decisions.md
- gurufinglobal/cosmos-sdk replace directives must stay
- x/bex, x/oracle, x/feepolicy are GURU custom — do NOT remove
- Run `go build ./...` after every sub-step

When completely finished, run:
openclaw system event --text "Track B complete: cosmos/evm v0.5.1 external dep migration done. Check track-b-plan.md for details." --mode now
