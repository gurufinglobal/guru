# Guru Oracle Module

The oracle module stores chain-approved oracle tasks and accepted oracle values.
Each validator's independent sidecar continuously collects and aggregates its
configured sources. The validator requests only due symbols, attaches the
sidecar's fresh aggregate results to CometBFT vote extensions, and the block
proposer turns valid vote extensions into a deterministic oracle proposal
payload.

## How It Works

1. The constitution moderator configures oracle parameters and tasks.
2. Each validator operator configures at least three local sources per sidecar
   feed. `oracled` polls them independently of node availability and persists a
   strict-majority median.
3. A participating validator looks up numeric tasks due for the current
   vote-extension height and asks its sidecar for those symbols only. It applies
   the on-chain `min_sources` threshold and writes valid aggregate results into
   its vote extension.
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
- `task_schedule`: exported schedule entries keyed by `(height, symbol)`.
  The reverse `(symbol, height)` keyset is reconstructed during import.

Symbols are normalized with `strings.TrimSpace` and uppercase before storage,
lookup, aggregation, and daemon matching.

Task scheduling is stored with composite keys. Exact due lookups use a prefixed
range over `(height, symbol)`, and vote-extension due lookups include the exact
vote-extension height plus missed buckets older than the one-block proposal
pipeline. The `height - 1` bucket is left alone because it may still be the
normal stale bucket that will be consumed by the next proposal. Task updates and
removals use the reverse `(symbol, height)` range to remove schedule entries
directly.

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

The oracle payload is expected only after vote extensions are enabled, the
current height is greater than the configured enable height, and at least one
task is due for the vote-extension height. The proposer must reserve proposal
bytes for the oracle payload before selecting normal txs. If the payload alone
exceeds `MaxTxBytes`, proposal preparation returns the payload and no normal txs.

Proposal verification checks:

- the payload height matches the block height;
- vote extension signatures and validator addresses validate through BaseApp;
- vote extension `BlockIdFlag` values match the current `LastCommit`;
- the payload values match locally recomputed aggregation output.

If BaseApp rejects an extended commit, Guru logs the block/header height,
canonical chain ID, extended-commit and last-commit rounds, vote count, and one
bounded tuple record per vote. Tuple records contain validator identity and
power, block-ID flag, byte lengths, SHA-256 hashes of the extension, signature,
canonical sign bytes, and public key, plus the result of diagnostic signature
verification. Raw vote-extension bytes, signatures, source responses, and
private keys are never logged. These records are emitted only after the
unchanged `baseapp.ValidateVoteExtensions` call returns an error; they do not
weaken or replace proposal validation.

Proposal height `H` aggregates vote extensions submitted at height `H-1`, so the
payload uses due tasks for `H-1`. Due tasks are the exact `H-1` bucket plus
missed buckets at `H-3` or older; the `H-2` bucket is preserved for the normal
one-block pipeline. When FinalizeBlock accepts the payload, the consumed due
buckets are removed and each task's schedule window is refilled from the
symbol's latest consumed due height. If a due task fails quorum or source
requirements, the empty payload is still accepted and the task waits until its
next interval instead of retrying every block.

## Usage Notes

Configure tasks with the moderator account, run the standalone `oracled`
process on validators that should participate, and set the validator node
oracle socket to its consumer Unix socket. The daemon does not discover or
adopt node tasks automatically; operators can run its read-only `reconcile`
command to compare local feeds with one node. A missing, stale, unavailable, or
under-quorum local aggregate is omitted, and a validator with no eligible
results emits an empty oracle vote extension without halting consensus.

CLI commands are available under `gurud tx oracle` and `gurud query oracle`.
Task creation uses numeric oracle tasks only:

```sh
gurud tx oracle update-params 3 3 100 --from <moderator>
gurud tx oracle upsert-task BTC/USD 5 --from <moderator>
gurud tx oracle remove-task BTC/USD --from <moderator>
gurud query oracle params
gurud query oracle active-tasks
gurud query oracle task BTC/USD
gurud query oracle latest-value BTC/USD
gurud query oracle latest-values
gurud query oracle history BTC/USD
```
