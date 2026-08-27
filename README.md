# Guru

Guru is a Cosmos SDK v0.53.6 application assembled on Cosmos EVM v0.6.2.

The repository owns the Guru application composition: keeper and module wiring,
genesis and transaction policy, Oracle consensus integration, and the operator
command tree. Cosmos SDK, Cosmos EVM, IBC-Go, and CometBFT remain upstream
dependencies, while Guru adds application-specific Constitution, Oracle,
staking, fee, and proposal behavior around them.

## Repository layout

```text
.
├── app/               # application, keepers, ante, consensus, and lifecycle
├── cmd/gurud/         # node and client command tree
├── config/            # product settings and network defaults
├── oracle/            # standalone oracled sidecar Go module
├── proto/             # Guru protobuf definitions
├── x/constitution/    # chain policy and minimum gas price scheduling
├── x/oracle/          # on-chain Oracle tasks, values, and consensus payloads
├── x/staking/         # staking wrapper enforcing minimum self-bond policy
├── Makefile
├── go.mod
└── go.sum
```

## Network defaults

`config/identity.go` centralizes Guru's compiled product identity and network
defaults. Chain IDs can be selected per network, while address prefixes, coin
type, native denomination, and related genesis policies are applied by the
application.

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
- `app.New` forwards the selected values to BaseApp, the Cosmos EVM keeper, and
  the EIP-712 compatibility layer.

Both IDs are transaction replay-protection domains. Every validator and client
on one network must therefore use the same values. Guru deliberately leaves
that network coordination to genesis/configuration management instead of
committing a second application-owned identity record.

## Application composition

Guru installs Cosmos SDK modules, Cosmos EVM VM/FeeMarket/ERC20 modules, IBC
core, and ICS-20. It also installs the Guru Constitution and Oracle modules and
wraps upstream staking so the chain-wide minimum validator self-bond is applied
at genesis and during validator-set updates. The ante handler preserves the
upstream Ethereum transaction path while applying Guru's standard `MsgSend`
gas and fee rules to eligible Cosmos transactions.

The generated default genesis:

- applies `agxn` to the recommended staking, mint, governance, FeeMarket, and
  EVM settings;
- requires explicit Constitution base and moderator addresses during `init`;
- configures FeeMarket with `no_base_fee = true`, `base_fee = 0`, and a positive
  chain-wide `min_gas_price`;
- starts with no active optional static precompiles;
- installs the upstream EIP-2935 history-storage preinstall;
- starts without ERC-20 token pairs or IBC clients/channels.

These are bootstrap values. Operators may edit a new network's genesis subject
to both upstream module validation and Guru's cross-module checks. Guru validates
the Constitution self-bond denomination and amount and the required FeeMarket
policy before accepting genesis.

## Mempool and proposal behavior

Guru deliberately uses the SDK `NoOpMempool` because CometBFT owns transaction
storage and gossip. The normal transaction selection path delegates to the SDK
default proposal handler for a no-op application mempool.

Guru wraps that default path with Oracle proposal handling. When an Oracle
payload is expected, `PrepareProposal` validates the extended commit, builds a
deterministic payload, reserves its proposal bytes, and prepends it to the normal
transactions. Every node recomputes the payload in `ProcessProposal` and rejects
a missing, malformed, or mismatched Oracle payload.

Broadcast transactions normally pass `CheckTx` before entering the CometBFT
mempool. Ante verification runs again during `FinalizeBlock`; an invalid or
stale normal transaction therefore produces a deterministic failed transaction
result without committing its state. Oracle consensus records are stricter: the
proposal is rejected when their canonical content or position is invalid.

Each validator may run the standalone `oracled` process and configure its Unix
socket in the node's `[oracle]` section. Oracle participation is enabled by
default, but an unavailable or disabled sidecar only omits that validator's
Oracle contribution and does not halt ordinary consensus. See the
[Oracle sidecar guide](oracle/README.md) and
[Oracle module guide](x/oracle/README.md).

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
make build

# Optional explicit build metadata
make build \
  VERSION=2.1.0 \
  ORACLE_VERSION=2.1.0 \
  COMMIT="$(git rev-parse HEAD)"

./build/gurud version --long --output json
./build/oracled --version
```

The build writes the node binary to `build/gurud` and the Oracle sidecar binary
to `build/oracled`.

## Local testnets

For the existing single-node development flow, use:

```bash
make local-node
```

This path keeps `local_node.sh` unchanged and does not start an Oracle sidecar.

For Oracle consensus validation, use the dedicated 4-validator harness:

```bash
make oracle-4v-testnet
```

The harness requires Bash, `curl`, `jq`, and Python 3. The Make target builds
`gurud` and `oracled` before starting the testnet. You can override the default
timeout and port range, for example:

```bash
WAIT_TIMEOUT=300 PORT_BASE=39000 make oracle-4v-testnet
```

It starts four `gurud` validators and four `oracled` sidecars on loopback
ports, requires each sidecar to have fresh BTC/USD, ETH/USD, and SOL/USD values
from at least three sources, then verifies four validators, block production,
sidecar reconciliation, and on-chain latest Oracle values. The script writes a
private base directory under the platform temporary directory, prints the
paths to `ready.env` and the stop file, and waits until the stop file exists or
it receives Ctrl-C. To stop it from another shell, create the printed stop file:

```bash
source /path/printed/by/the/harness/ready.env
touch "$BASE/stop"
```

For a one-shot end-to-end readiness and consensus check:

```bash
make oracle-4v-smoke
```

The harness uses the development `test` keyring and public HTTPS price sources,
so it is intended for local and testnet validation only. It is deliberately not
part of the default CI or Docker workflows: provider availability, outbound
network policy, or rate limiting can make it fail independently of the code.
On failure, inspect the printed base directory, especially its `logs/` files.
The harness does not delete that directory automatically, and it does not
validate the TRX/USD dynamic minimum gas price path used by Constitution.

## Docker

The repository includes a non-root, Debian-based `gurud` image and Compose
configurations for local development and testnet nodes. Node state, genesis,
keys, and configuration live in a persistent named volume and are never baked
into the image.

```bash
make docker-check

# After initializing the persistent volume as described in the guide:
docker compose -f compose.yaml -f compose.local.yaml up --detach --no-build
docker compose -f compose.yaml -f compose.local.yaml ps
```

The base Compose model publishes only P2P. The local override binds RPC-facing
ports to host loopback, with REST and JSON-RPC disabled by default. See
[Run a Guru node with Docker](docs/docker.md) for initialization, existing
testnet genesis installation, port policy, lifecycle commands, backups, and the
end-to-end integration check.

## Release artifacts

Tagged releases are published on the
[GitHub Releases page](https://github.com/gurufinglobal/guru/releases) with
checksums. `gurud` archives are built for Linux AMD64/ARM64, macOS AMD64/ARM64,
and Windows AMD64. `oracled` archives are built for Linux and macOS on AMD64 and
ARM64; Windows is not supported because the sidecar relies on Unix sockets and
POSIX filesystem semantics.

## Initialize a network

The following example creates a development validator. It creates separate
Constitution base and moderator accounts before `init` because both addresses
are required in genesis. The `test` keyring stores private keys without
production-grade protection; securely record the generated mnemonics and do not
use this setup for production keys.

```bash
export GURU_HOME="$PWD/.local/guru"

./build/gurud keys add constitution-base \
  --algo eth_secp256k1 \
  --keyring-backend test \
  --home "$GURU_HOME"

./build/gurud keys add constitution-moderator \
  --algo eth_secp256k1 \
  --keyring-backend test \
  --home "$GURU_HOME"

CONSTITUTION_BASE_ADDRESS="$(
  ./build/gurud keys show constitution-base \
    --address \
    --keyring-backend test \
    --home "$GURU_HOME"
)"

CONSTITUTION_MODERATOR_ADDRESS="$(
  ./build/gurud keys show constitution-moderator \
    --address \
    --keyring-backend test \
    --home "$GURU_HOME"
)"

./build/gurud init validator-0 \
  --chain-id guru_631-1 \
  --constitution-base-address "$CONSTITUTION_BASE_ADDRESS" \
  --constitution-moderator-address "$CONSTITUTION_MODERATOR_ADDRESS" \
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

./build/gurud genesis add-genesis-account \
  "$CONSTITUTION_BASE_ADDRESS" \
  100000000000000000000agxn \
  --home "$GURU_HOME"

./build/gurud genesis add-genesis-account \
  "$CONSTITUTION_MODERATOR_ADDRESS" \
  100000000000000000000agxn \
  --home "$GURU_HOME"

./build/gurud genesis gentx \
  validator \
  1000000000000000000agxn \
  --chain-id guru_631-1 \
  --gas 200000 \
  --gas-prices 630000000000agxn \
  --keyring-backend test \
  --home "$GURU_HOME"

./build/gurud genesis collect-gentxs --home "$GURU_HOME"
./build/gurud genesis validate --home "$GURU_HOME"
```

The gentx gas price matches Guru's default genesis FeeMarket minimum. A lower
fee may pass file-level genesis validation but will fail when the application
executes the gentx during `InitChain`.

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
- preserve the genesis FeeMarket policy (`no_base_fee = true`, `base_fee = 0`,
  and a positive chain-wide `min_gas_price`) and configure non-zero local
  `minimum-gas-prices` consistently across validators;
- secure the Constitution base and moderator accounts and operate validator
  Oracle sidecars with reviewed, independent data sources;
- set finite query/RPC limits and expose only services required by each node
  role;
- validate pruning, snapshots, state sync, indexing, backups, sentry topology,
  remote signing, monitoring, and restore procedures under representative
  multi-validator load.

These deployment choices are intentionally not hard-coded in the application.

## Contributing and conduct

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development and pull request
workflow. All project participation is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md).
