# Guru Oracle Daemon

`oracled` is the local sidecar daemon used by validators that participate in the
Guru oracle. It does not write chain state directly. It serves source samples to
the validator process over a Unix domain socket, and the validator includes
derived oracle results in CometBFT vote extensions.

## How It Works

1. `oracled start` loads `oracled.toml`.
2. Before serving, it queries the connected node gRPC endpoint for oracle params
   and active oracle tasks.
3. It checks that configured local sources match active numeric node tasks by
   normalized symbol and value type, and that at least one matched task has
   enough sources to satisfy the chain `min_sources` parameter.
4. It opens a gRPC server on the configured Unix socket.
5. When the validator asks for samples for the tasks due at that
   vote-extension height, the daemon fetches every matching HTTP source
   concurrently, extracts the configured JSON path, and returns grouped samples
   by symbol.
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

## Operational Checks

On startup, the daemon fails fast when:

- the config is invalid;
- the node gRPC endpoint cannot return oracle params;
- the node has no active numeric oracle tasks;
- none of the configured sources match active oracle tasks;
- configured sources do not satisfy `min_sources` for any active task;
- the socket path cannot be created or an existing non-socket file is present.

The daemon removes stale socket files only when the existing path is a Unix
socket.
