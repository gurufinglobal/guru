# Guru

Guru is a Cosmos SDK v0.53.6 application assembled on Cosmos EVM v0.6.2.

The repository owns the Guru application composition: keeper and module
wiring, genesis defaults, and the operator command tree. Cosmos SDK, Cosmos
EVM, IBC-Go, CometBFT, and their module behavior are consumed as upstream
dependencies and are not locally forked or policy-wrapped here.

## Repository layout

```text
.
├── app/               # application, keepers, modules, and lifecycle
├── cmd/gurud/         # node and client command tree
├── config/            # product settings and network defaults
├── Makefile
├── go.mod
└── go.sum
```

## Network defaults

The values in `config/identity.go` are defaults for newly created
configuration and genesis files. They are not persisted in a Guru-owned
identity store and are not immutable policy enforced by the binary.

| Property | Default |
|---|---|
| Binary and BaseApp name | `gurud` |
| Home | `~/.gurud` |
| Cosmos chain ID | `guru_631-1` |
| EIP-155 chain ID | `631` |
| Account prefix | `guru` |
| Validator prefix | `guruvaloper` |
| Consensus prefix | `guruvalcons` |
| Base denomination | `agxn` |
| Display denomination | `gxn` |
| Denomination exponent | `18` |
| BIP-44 coin type | `60` |

The same source and binary can be configured for another network:

- the Cosmos chain ID is selected by an explicit command value when present,
  otherwise from `config/genesis.json`;
- the EVM chain ID is selected from `app.toml` or
  `--evm.evm-chain-id`, with `631` as the fallback;
- `app.New` forwards those values to BaseApp, the Cosmos EVM keeper, and the
  EIP-712 compatibility layer;
- genesis validation delegates to the upstream module validators and does not
  impose Guru-only denomination or governance locks.

Both IDs are transaction replay-protection domains. Every validator and client
on one network must therefore use the same values. Guru deliberately leaves
that network coordination to genesis/configuration management instead of
committing a second application-owned identity record.

## Application composition

Guru installs the Cosmos SDK modules, Cosmos EVM VM/FeeMarket/ERC20 modules,
IBC core and ICS-20 modules, and their upstream message/query services directly.
There are no Guru wrappers that constrain governance parameter updates or
attempt to repair behavior inside those dependencies.

The generated default genesis:

- applies `agxn` to the recommended staking, mint, governance, FeeMarket, and
  EVM settings;
- starts with no active optional static precompiles;
- installs the upstream EIP-2935 history-storage preinstall;
- starts without ERC-20 token pairs or IBC clients/channels.

These are bootstrap values. Operators may edit a new network's genesis subject
to the corresponding upstream module validation rules.

## Mempool and proposal behavior

Guru deliberately uses the SDK `NoOpMempool` because CometBFT owns transaction
storage and gossip. Guru leaves the SDK's default proposal handlers unchanged.
For a no-op application mempool, the default `PrepareProposal` selects the
transactions supplied by CometBFT without repeating ante verification, and the
default `ProcessProposal` accepts the proposal.

Broadcast transactions normally pass `CheckTx` before entering the CometBFT
mempool. Ante verification runs again during `FinalizeBlock`; an invalid or
stale transaction therefore produces a deterministic failed transaction result
without committing its state. Guru does not add a stricter application-specific
proposal acceptance policy. A proposer can consequently waste its proposal
capacity with failing transactions, which is the upstream availability and
throughput trade-off of this configuration rather than a separate state-safety
rule.

## RPC behavior

Guru uses the Cosmos EVM v0.6.2 server implementation without a local RPC
quarantine or lifecycle patch. The generated application configuration keeps
the upstream service defaults, including JSON-RPC disabled by default.
Operators may enable JSON-RPC, WebSocket, the custom indexer, gRPC, or REST
through the normal upstream `app.toml` and command flags.

For production, place public endpoints behind appropriate network controls and
set finite connection, batch, response, timeout, filter, log, block-range, and
query-gas limits for the node role.

## Build

The module declares Go `1.23.8`.

```bash
make build VERSION=<version> COMMIT=<git-commit>
./build/gurud version --long --output json
```

The binary is written to `build/gurud`.

## Initialize a network

The following example creates a development validator. The `test` keyring
stores private keys without production-grade protection.

```bash
export GURU_HOME="$PWD/.local/guru"

./build/gurud init validator-0 \
  --chain-id guru_631-1 \
  --home "$GURU_HOME"

./build/gurud keys add validator \
  --algo eth_secp256k1 \
  --keyring-backend test \
  --home "$GURU_HOME"

VALIDATOR_ADDRESS="$(
  ./build/gurud keys show validator \
    --address \
    --keyring-backend test \
    --home "$GURU_HOME"
)"

./build/gurud genesis add-genesis-account \
  "$VALIDATOR_ADDRESS" \
  1000000000000000000000agxn \
  --home "$GURU_HOME"

./build/gurud genesis gentx \
  validator \
  1000000000000000000agxn \
  --chain-id guru_631-1 \
  --gas 200000 \
  --gas-prices 1agxn \
  --keyring-backend test \
  --home "$GURU_HOME"

./build/gurud genesis collect-gentxs --home "$GURU_HOME"
./build/gurud genesis validate --home "$GURU_HOME"
```

For another network, use its Cosmos chain ID during initialization and set the
chosen EVM chain ID in every validator's `app.toml`:

```toml
[evm]
evm-chain-id = 9631
```

Start the node:

```bash
./build/gurud start --home "$GURU_HOME"
```

## Export

Normal-height export is supported. Zero-height rewriting and a jail allowlist
are not implemented by this bootstrap application.

```bash
./build/gurud export \
  --home "$GURU_HOME" \
  --output-document exported-genesis.json

./build/gurud genesis validate exported-genesis.json \
  --home "$GURU_HOME"
```

## Mainnet configuration notes

Generated configuration is a bootstrap template, not a mainnet profile.
Before launch:

- choose a finite CometBFT `consensus.params.block.max_gas` based on measured
  execution capacity; do not use unlimited gas for production;
- keep FeeMarket enforcement and a non-zero base fee, and configure non-zero
  local `minimum-gas-prices` consistently across validators;
- set finite query/RPC limits and expose only services required by each node
  role;
- validate pruning, snapshots, state sync, indexing, backups, sentry topology,
  remote signing, monitoring, and restore procedures under representative
  multi-validator load.

These deployment choices are intentionally not hard-coded in the application.
