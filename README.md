# Guru V3 Development Branch

This branch is currently being used for Guru V3 development.

## Oracle sidecar

`make build` produces both `build/gurud` and the independently versioned
`build/oracled`. The sidecar is a standalone Go module under `oracle/`; it uses
the root-generated internal gogo Oracle types for node-facing gRPC and does not
maintain its own protobuf tree.

`oracled` collects configured HTTPS feeds without depending on a running node,
persists strict-majority local aggregates, and serves only fresh aggregate
values over its consumer Unix socket. The validator sends due symbols only and
continues consensus with an empty or partial vote extension when the sidecar or
an aggregate is unavailable. See [oracle/README.md](oracle/README.md) for the
configuration, lifecycle, status, history, and read-only reconcile runbook.

## EVM JSON-RPC indexer storage

Validators and P2P-only sentries do not need the custom EVM transaction
indexer and should keep `json-rpc.enable-indexer = false`.

Guru currently recommends GoLevelDB for public or private RPC nodes that enable
the custom EVM indexer. Configure the backend before the node's first start:

```toml
app-db-backend = "goleveldb"

[json-rpc]
enable = true
enable-indexer = true
```

Ensure that the node's service arguments do not override this setting with a
different `--app-db-backend` value.

Cosmos EVM v0.7.1 uses `app-db-backend` for the application, snapshot metadata,
and EVM indexer databases; the indexer backend cannot be selected separately.
Do not change the backend in place for an existing node home. Create a fresh
home and fully sync the node instead.

This is an operational workaround for the observed Cosmos EVM v0.7.x indexer
shutdown and persistence behavior. Guru verifies graceful restart and offline
EVM index reconstruction with GoLevelDB, but this does not fix the upstream
indexer lifecycle and progress-tracking defects or guarantee durability across
machine or power failure. RPC operators should retain enough block history to
rebuild the local EVM index.
