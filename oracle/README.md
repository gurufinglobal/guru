# Guru Oracle Sidecar

`oracled` is the validator-local collector for the Guru Oracle. It is a
standalone Go module and foreground process. It continuously collects configured
HTTPS feeds, computes the first strict-majority median, persists successful
aggregates, and serves fresh aggregates to one local validator over a Unix
socket.

The sidecar never writes chain state. The node still chooses due symbols,
enforces on-chain `min_sources`, signs vote extensions, and participates in
cross-validator proposal aggregation and verification. A stopped sidecar,
failed source, stale value, or reconciliation failure reduces that validator's
Oracle contribution but does not halt ordinary consensus.

## Build

From the repository root:

```sh
make build
```

To build the nested module directly from this monorepo:

```sh
CGO_ENABLED=0 GOWORK=off go -C oracle build -mod=readonly ./cmd/oracled
```

The module intentionally requires the Guru root module through
`replace github.com/gurufinglobal/guru/v3 => ..`. Handwritten sidecar code
imports only the root-generated internal gogo Oracle types for node-facing
gRPC. Admin and storage protocols are sidecar-native and use no private
protobuf tree.

## Home and commands

The default home is `$HOME/.oracled`. `--home <path>` replaces it completely:

```text
.oracled/
├── config.toml
├── sources.toml
├── data/
│   ├── oracle.db
│   └── storage.meta
├── logs/
└── run/
    ├── oracled.lock
    ├── oracle.sock
    └── admin.sock
```

Initialize a fresh unpublished sidecar home:

```sh
oracled --home /srv/guru/oracle init
```

Validate and start it:

```sh
oracled --home /srv/guru/oracle validate
oracled --home /srv/guru/oracle start
```

`start` stays in the foreground. A service manager owns restarts, output
capture, and rotation. The six product commands are:

- `init`
- `validate`
- `start`
- `status [--format text|json]`
- `history <SYMBOL> [--page-size 1..50] [--page-key TOKEN] [--offline]`
- `reconcile --node-grpc HOST:PORT [--format text|json]`

Only `reconcile` connects to a Guru node. Long-running collection and both
local services start without node gRPC.

Each validator operates its own sidecar and selects its own source providers,
topology, feed intervals, and freshness policy. Guru does not require or compare
identical sidecar configurations across validators. The common on-chain view is
limited to active Oracle tasks and acceptance parameters; source suitability and
provider independence remain local operator responsibilities.

## Configuration publication

`config.toml` and `sources.toml` are one fail-closed publication pair. Both use
schema version 1 and the same `publication_revision`. `config.toml` also
contains the SHA-256 digest of the exact `sources.toml` bytes. Unknown fields,
unsupported versions, a revision mismatch, or a digest mismatch are rejected.

Publish an update by writing and fsyncing temporary files in the same
directories, atomically renaming `sources.toml` first, atomically renaming
`config.toml` last, running `validate`, and gracefully restarting `oracled`.
Temporary and final configuration files must be mode `0600`; the home and all
of its directories must be mode `0700`. Advance `publication_revision` for
every published update. There is no runtime hot reload.

The initialized process configuration is:

```toml
schema_version = 1
publication_revision = "<generated>"
sources_sha256 = "<exact lowercase SHA-256>"

[server]
consumer_socket = "run/oracle.sock"
admin_socket = "run/admin.sock"
max_request_bytes = 65536
max_response_bytes = 1048576

[collector]
max_concurrency = 32
source_response_bytes = 1048576
max_redirects = 3
max_attempts = 3
request_timeout = "5s"
connect_timeout = "2s"
tls_handshake_timeout = "2s"
response_header_timeout = "3s"
retry_initial_backoff = "100ms"
retry_max_backoff = "1s"

[storage]
database = "data/oracle.db"
marker = "data/storage.meta"
lock = "run/oracled.lock"
history_retention = 30

[runtime]
shutdown_timeout = "10s"

[logging]
level = "info"
format = "text"
```

The request and response limits are fixed protocol envelopes, not tuning
knobs. The ownership lock is likewise fixed at `run/oracled.lock` so mutable
configuration cannot create two lock domains for one home.

`init` writes an empty feed list. Operators add at least three independently
selected sources per feed:

```toml
schema_version = 1
publication_revision = "<same revision>"

[[feeds]]
symbol = "BTC/USD"
interval = "10s"
stale_after = "30s"

[[feeds.sources]]
id = "provider-a"
url = "https://provider-a.example/value"
json_pointer = "/data/price"

[[feeds.sources]]
id = "provider-b"
url = "https://provider-b.example/value"
json_pointer = "/data/price"

[[feeds.sources]]
id = "provider-c"
url = "https://provider-c.example/value"
json_pointer = "/data/price"
```

Sources are public HTTPS GET endpoints with RFC 6901 JSON Pointers. Credentials,
fragments, HTTP downgrade redirects, POST, scripts, authentication, and
provider-specific product branches are rejected or unsupported. Duplicate IDs
and URLs are rejected, but configuration strings cannot prove provider
independence. Source trust and genuine diversity remain operator
responsibilities.

`SSL_CERT_FILE` may name a bounded PEM CA bundle. It is appended to system
roots on Linux and macOS; hostname and certificate verification remain
enabled.

## Collection and freshness

Each feed starts immediately after both sockets are ready and then follows its
own monotonic interval. All configured sources are admitted concurrently under
the global bound. Effective source concurrency is also limited so configured
response ceilings cannot place more than 32 MiB of raw response bodies in
flight; the initialized `32 × 1 MiB` defaults are unchanged. A cycle waits for
all terminal source results or its fixed deadline; reaching the first quorum
does not finish it. Deadline cancellation covers semaphore waits, requests,
retry sleeps, JSON decoding, and retries. Missed ticks are skipped and a feed
never overlaps itself.

Local quorum is always `floor(source_count/2)+1`. Every successful source has
equal weight. Odd medians select the middle fixed-18 value. Even medians use the
overflow-safe, exact, toward-zero mean of the middle two values. Values are
never parsed through binary floating point and excess precision is rejected,
not rounded.

A failed or under-quorum cycle stores nothing and does not erase the previous
successful aggregate. That older aggregate remains eligible only while it is
fresh and belongs to the current activation generation. The exact stale
boundary is `age >= stale_after`. Future persisted timestamps and unsafe clock
classification are never served.

The node requests only normalized, sorted symbols from `oracle.sock`.
`GetAggregates` performs one bounded in-memory pass under a short read lock: no
HTTP, database, history, configuration, or node query occurs on that path.
Unknown, unconfigured, no-value, under-quorum, stale, or clock-anomalous
symbols are omitted. The sidecar may return a strict-majority value below chain
`min_sources`; the node alone applies that on-chain threshold. The consumer
listener, concurrent HTTP/2 streams, metadata, request, and response sizes are
all bounded independently.

## Status, history, and reconciliation

`admin.sock` is a separate owner-only HTTP/JSON Unix service with exactly:

- `GET /v1/status`
- `GET /v1/history?symbol=...&page_size=...&page_key=...`

Status never exposes aggregate values, raw source observations, response
bodies, URLs, pointers, headers, or credentials. History contains only
successful aggregate values and bounded provenance. Page tokens are
authenticated, snapshot high/low water, and expire after daemon restart or
retention movement.

Live history is the default. `history --offline` explicitly acquires the same
exclusive home lock, proves no writer is active, verifies the full database,
reads one page, and closes it. It never silently falls back from live access.

`reconcile` is a one-shot, read-only comparison with node `Params` and all
paginated `ActiveTasks`. It checks active-symbol coverage and the running
sidecar's live contribution readiness, including configured and successful
source counts, freshness, current aggregate availability, and whether the
running process uses the currently published local configuration pair. Exit
codes are:

- `0`: authoritative report with no blocking readiness mismatch
- `1`: authoritative report with one or more blocking mismatches
- `2`: configuration, sidecar, node, transport, or protocol failure

Extra locally configured inactive feeds are informational because collection is
intentionally node-independent. Reconciliation never edits configuration,
writes a transaction, changes a task, injects a value, or restarts a process.
It reports a blocking `runtime_config_mismatch` when the running daemon's
publication revision or source digest differs from the published pair on disk.
`Blocking` and exit code `1` mean only that this validator needs local operator
action before it is ready to contribute for every active task. They do not stop
the sidecar or node, reject a block, change chain state, or impose one
validator's source configuration on another validator.

## Storage and restart policy

Successful aggregates and provenance are committed synchronously to bbolt
before publication to the consumer snapshot. Default retention is 30 records
per currently configured symbol across generations. Plan changes increment an
activation generation; an `A -> B -> A` sequence cannot resurrect the first A
value. Removing a symbol deletes its history during atomic plan activation.
Raw source observations and bodies are never persisted.

One process owns one home through `run/oracled.lock`. Startup refuses partial
database/marker pairs, corruption, unknown schemas or records, live socket
collisions, symlinks, and non-socket collisions. It removes only an
unresponsive path that is itself a Unix socket. `SIGINT` and `SIGTERM` stop new
cycles, cancel and join in-flight work, close both services, sync storage, and
release the lock within the configured shutdown deadline. If a lower-level
operation such as filesystem sync cannot finish by that deadline, a process
watchdog exits with status 1 instead of waiting indefinitely; the operating
system then closes descriptors and releases the home lock, but an explicit
final sync may not have completed. A second termination signal uses the
operating system's default immediate behavior.

bbolt is host-endian local state and does not shrink its file in place; bounded
retention stabilizes reuse at a physical high-water mark rather than reducing
file size. Persistent guarantees remain limited by the filesystem, kernel,
storage hardware, and power-loss behavior. On Linux 5.10 through 5.16 with
ext4, run with `fast_commit` disabled unless the kernel contains the upstream
fix.

Node and sidecar processes may restart independently. Requests while the
sidecar is down produce the node's normal empty Oracle contribution. Rolling
sidecar configuration across validators may temporarily reduce Oracle quorum,
so coordinate restarts with the on-chain validator threshold in mind.

This is a pre-publication clean break. Deploy matching node and sidecar
binaries on a fresh network and use fresh sidecar homes. Old RPCs,
configurations, databases, mixed versions, and pre-change Oracle block replay
are not supported.
