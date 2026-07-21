# Guru Oracle Daemon

`oracled` is the local sidecar daemon used by validators that participate in the
Guru oracle. It does not write chain state directly. It serves source samples to
the validator process over a Unix domain socket, and the validator includes
derived oracle results in CometBFT vote extensions.

## How It Works

1. `oracled start` loads `oracled.toml`.
2. It validates the local configuration and immediately opens a gRPC server on
   the configured Unix socket. The node does not need to be running first.
3. In parallel, it queries the connected node gRPC endpoint for oracle params
   and active tasks. Failed queries retry with exponential backoff from 250 ms
   to a maximum of 5 seconds until the daemon stops.
4. Once preflight succeeds, it checks that configured local sources match active
   numeric node tasks and that at least one task satisfies the chain
   `min_sources` parameter. Interval-based source pollers then start exactly
   once.
5. When the validator asks for samples for the tasks due at that
   vote-extension height, the daemon fetches every matching HTTP source
   concurrently, extracts the configured JSON path, and returns grouped samples
   by symbol. The tasks in this request remain authoritative, including while
   node preflight is degraded.
6. The validator computes deterministic per-symbol medians from returned samples
   and writes them into the vote extension if each result has enough sources.

The daemon follows paginated node task responses, so it can validate against more
than the module query default page size. Per-task `submission_interval` is
enforced by the chain node before it calls the daemon; `oracled` only serves the
task list included in each request.

## Commands

Build all command binaries from the repository root:

```sh
make build
```

Write a default config:

```sh
build/oracled init-config
```

Start the daemon:

```sh
build/oracled start
```

Useful flags:

- `--home`: oracle daemon home directory. Defaults to `.oracled` under the
  platform-specific user home.
- `--config`: explicit config file path.
- `--socket`: override the Unix socket path from config.
- `--force`: overwrite an existing config during `init-config`.

## Configuration

Default config path:

```text
$ORACLED_HOME/config/oracled.toml
```

Example:

```toml
socket = "/path/to/oracle/oracle.sock"
request_timeout = "2s"
source_timeout = "500ms"
node_grpc = "127.0.0.1:9090"
node_query_timeout = "2s"

[[sources]]
name = "coinbase-btc-usd"
symbol = "BTC/USD"
value_type = "NUMERIC"
url = "https://example.invalid/prices/{symbol}"
response_path = "data.price"
timeout = "300ms"
interval = "1s"

[sources.headers]
Authorization = "Bearer token"
```

Fields:

- `socket`: Unix socket used by the validator process.
- `request_timeout`: total timeout for one validator sample request.
- `source_timeout`: default timeout for each HTTP source request.
- `node_grpc`: node gRPC endpoint used for preflight task discovery.
- `node_query_timeout`: timeout for node gRPC queries.
- `sources`: local HTTP source definitions.

Source fields:

- `name`: unique source name per symbol.
- `symbol`: oracle symbol. Matching is trim plus uppercase normalized.
- `value_type`: `NUMERIC` only. `STRING` and `BOOL` are reserved for future
  non-numeric aggregation and are rejected by the current daemon and module
  validation.
- `url`: HTTP GET URL. `{symbol}` is replaced with a URL-escaped task symbol.
- `response_path`: dot-separated JSON path to extract.
- `timeout`: optional per-source timeout override.
- `interval`: optional source refresh interval. When set, `oracled` polls and
  caches the source independently from validator sample requests.
- `headers`: optional HTTP headers.

## Validator Integration

Run `oracled` on the same host as the validator and configure the validator node
oracle socket to the same Unix socket. The chain node controls whether the local
validator participates in oracle vote extensions. For sources with `interval`,
the daemon keeps a local fresh cache; for other sources, it fetches on each
sample request.

The daemon silently ignores failed HTTP sources for a request. A symbol is useful
only when enough configured sources return values for the validator to satisfy
the module `min_sources` parameter.

### Node gRPC requirement

New Guru node homes enable the gRPC query server in the generated `app.toml`.
For an existing home, confirm this setting before relying on interval pollers or
the `ready` state:

```toml
[grpc]
enable = true
address = "localhost:9090"
```

`node_grpc` in `oracled.toml` must resolve to that listener. Keeping gRPC bound
to loopback is recommended when the daemon runs on the validator host.

Guru derives the SDK chain ID from `config/genesis.json` when the node starts.
An explicit `--chain-id` is optional, but, when present, it must match genesis;
the node rejects a mismatch instead of validating vote extensions under the
wrong canonical signing tuple.

## Runtime States

The daemon reports its state through bounded logs:

- `state=serving`: the Unix socket is accepting validator requests. This state
  does not imply that node preflight has completed.
- `node_preflight=degraded`: the node is unavailable or its Oracle tasks and
  the local source configuration are not yet compatible. The log includes the
  bounded retry delay and query error. The daemon remains alive and serving.
- `node_preflight=ready`: params and active tasks were validated. This includes
  the attempt count and task count.
- `state=ready`: interval source pollers have received the validated task set.

A daemon can remain degraded indefinitely without stopping block production.
Fix the node gRPC listener, endpoint, task state, or source configuration; the
same process retries and becomes ready without a restart.

## Restart Runbook

The node and sidecar may start and restart independently in either order. Use
the existing node home, application database, CometBFT WAL, and private
validator state.

1. Prefer `SIGTERM` for planned maintenance and wait for the process to exit.
   `SIGKILL` recovery is supported, but it cannot provide a graceful flush.
2. Start either `gurud` or `oracled`. Do not wait for the other process merely
   to establish the sidecar socket.
3. Confirm `state=serving`. After the node gRPC service is reachable, confirm
   `node_preflight=ready` and `state=ready`.
4. Check chain progress and Oracle state with:

   ```sh
   gurud status
   gurud query oracle active-tasks
   gurud query oracle latest-values
   ```

5. If block production continues but Oracle values do not advance, verify the
   task submission interval, active validator participation, source responses,
   and the module `min_sources` and `min_validators` thresholds.

Never delete or rewrite the WAL, application state, block store, or
`priv_validator_state.json` as a restart remedy. Preserve the first failure
logs and home for diagnosis if the node reports an Oracle vote-extension
validation failure.

When a validator cannot obtain enough local samples, it emits an empty Oracle
vote extension. Consensus can continue; if fewer than `min_validators` provide
usable Oracle results, Oracle state pauses until a later valid task window while
ordinary blocks continue.

## Operational Checks

On startup, the daemon fails fast when:

- the config is invalid;
- the socket path cannot be created or an existing non-socket file is present.

Node query failures, no active numeric task, source/task mismatches, and
insufficient configured sources are degraded preflight conditions. They retry
without closing the socket and become ready when the condition is corrected.

The daemon removes stale socket files only when the existing path is a Unix
socket.
