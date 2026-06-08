# Guru Oracle Module

The oracle module stores chain-approved oracle tasks and accepted oracle values.
Validators collect samples through their local oracle sidecar, attach compact
results to CometBFT vote extensions, and the block proposer turns valid vote
extensions into a deterministic oracle proposal payload.

## How It Works

1. The constitution moderator configures oracle parameters and tasks.
2. Each validator that enables local oracle participation looks up the tasks due
   for the current vote-extension height and asks its `oracled` sidecar for
   samples for only those tasks.
3. The validator writes median local results into its vote extension.
4. The proposer validates the extended commit, aggregates validator results, and
   prepends one oracle payload transaction to the proposal when vote extensions
   are enabled for the height.
5. Every node recomputes the payload during proposal processing. A proposal is
   rejected if the payload is missing, malformed, too large for the proposal
   limit, uses mismatched commit flags, or does not match the recomputed values.
6. FinalizeBlock applies accepted oracle values to latest-value state and bounded
   per-symbol history.

The module does not have an on-chain `enabled` parameter. Once the module is
wired into the app, oracle payload expectation is controlled by CometBFT vote
extension enable height. Individual validators can still disable local oracle
sidecar participation through their node configuration.

## State

- `Params`: `min_validators`, `min_sources`, and `history_limit`.
- `OracleTask`: symbol, value type, per-task enabled flag, and
  `submission_interval` in block heights.
- `OracleValue`: latest accepted value per symbol.
- `OracleHistory`: bounded accepted value history per symbol.
- task schedule index: internal `Collections.KeySet` entries keyed by
  `(height, symbol)` and a reverse `(symbol, height)` keyset.

Symbols are normalized with `strings.TrimSpace` and uppercase before storage,
lookup, aggregation, and daemon matching.

Task scheduling is stored with composite keys. Checking whether the current
height has oracle work uses a prefixed range over `(height, symbol)`, so it only
iterates symbols due at that height. Task updates and removals use the reverse
`(symbol, height)` range to remove stale schedule entries directly.

## Parameters

- `min_validators`: minimum number of validator results required before a symbol
  can be accepted in a proposal payload.
- `min_sources`: minimum source count required inside each validator result.
- `history_limit`: maximum number of historical values retained per symbol.

All parameters must be positive. Parameter updates are accepted only from the
current constitution moderator address.

## Messages

- `MsgUpdateParams`: update oracle params.
- `MsgUpsertTask`: add or update an oracle task.
- `MsgRemoveTask`: remove an oracle task by symbol.

Task updates and removals are also moderator-only.
Configured tasks must have a positive `submission_interval`.

## Queries

- `Params`: returns oracle parameters.
- `ActiveTasks`: returns enabled oracle tasks.
- `Task`: returns one task by symbol.
- `LatestValue`: returns the latest accepted value for a symbol.
- `LatestValues`: returns latest accepted values.
- `History`: returns bounded history for a symbol.

`ActiveTasks`, `LatestValues`, and `History` are paginated. If the request does
not set `pagination.limit`, the module returns 30 items. Clients can request a
different page size with `pagination.limit`, use `pagination.offset`, or pass the
previous response `pagination.next_key` as `pagination.key`.

## Proposal Payload Rules

The oracle payload is expected only after vote extensions are enabled and the
current height is greater than the configured enable height. The proposer must
reserve proposal bytes for the oracle payload before selecting normal txs. If the
payload alone exceeds `MaxTxBytes`, proposal preparation returns the payload and
no normal txs.

Proposal verification checks:

- the payload height matches the block height;
- vote extension signatures and validator addresses validate through BaseApp;
- vote extension `BlockIdFlag` values match the current `LastCommit`;
- the payload values match locally recomputed aggregation output.

Proposal height `H` aggregates vote extensions submitted at height `H-1`, so the
payload uses the task schedule for `H-1`. When FinalizeBlock accepts the payload,
the consumed schedule bucket is advanced by each task's `submission_interval`.
The scheduler keeps the next needed height available before the corresponding
`ExtendVote` call observes state, accounting for the one-block vote-extension
pipeline.

## Usage Notes

Configure tasks with the moderator account, run `oracled` on validators that
should participate, and set the validator node oracle socket to the daemon Unix
socket. A validator with no matching local sources simply emits an empty oracle
vote extension.
