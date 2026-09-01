# Run a Guru node with Docker

This guide is for local development and testnet nodes. It builds `gurud` from
the current checkout and stores all node state in a Docker named volume. It does
not publish an image, configure a production validator, or run `oracled`.

## Prerequisites

- Docker Engine 24 or later with Docker Compose v2
- BuildKit (enabled by default in current Docker Desktop and Docker Engine)
- Enough disk space for the Go build cache, image, and chain data

The image supports Linux AMD64 and ARM64. `gurud` is CGO-enabled, so the final
image uses a Debian/glibc runtime instead of Alpine.

## Build and verify the image

Copy the non-secret configuration template if you want to change the defaults:

```bash
cp .env.example .env
set -a
. ./.env
set +a
```

Compose reads `.env` automatically; sourcing it also makes the same values
available to the initialization commands shown below. Keep this file free of
mnemonics, private keys, and credentials.

Build the native architecture image and run its static checks:

```bash
make docker-check
```

This verifies both Compose models, embeds the current Git version and commit,
checks `gurud version`, confirms the runtime user is not root, and confirms that
the compiler and source tree are absent from the final image.

To build one different architecture, use:

```bash
make docker-build DOCKER_PLATFORM=linux/amd64
```

To build both supported architectures into a local OCI archive without
publishing it:

```bash
make docker-build-multiarch
```

The multi-architecture build writes `build/guru-node.oci.tar` by default.

## Initialize a new local or testnet network

Initialization is intentionally separate from startup. The following helper
runs one-off `gurud` commands against the same `guru-data` named volume:

```bash
docker_gurud() {
  docker compose run --rm --no-deps -T gurud "$@"
}
```

Create the required Constitution accounts. These commands print development
mnemonics. Store them securely and never reuse them for production or accounts
holding assets of value.

```bash
docker_gurud keys add constitution-base \
  --algo eth_secp256k1 \
  --keyring-backend test

docker_gurud keys add constitution-moderator \
  --algo eth_secp256k1 \
  --keyring-backend test

CONSTITUTION_BASE_ADDRESS="$(
  docker_gurud keys show constitution-base \
    --address \
    --keyring-backend test
)"

CONSTITUTION_MODERATOR_ADDRESS="$(
  docker_gurud keys show constitution-moderator \
    --address \
    --keyring-backend test
)"
```

Initialize the node home using the chain ID from `.env` (or the compiled
default) and the two required addresses:

```bash
GURU_CHAIN_ID="${GURU_CHAIN_ID:-guru_631-1}"

docker_gurud init validator-0 \
  --chain-id "$GURU_CHAIN_ID" \
  --constitution-base-address "$CONSTITUTION_BASE_ADDRESS" \
  --constitution-moderator-address "$CONSTITUTION_MODERATOR_ADDRESS"
```

Fresh `gurud init` always writes Vote Extensions as disabled
(`consensus.params.abci.vote_extensions_enable_height = 0`) and exposes no
activation-height override. The value is not duplicated in `app.toml`; after
`InitChain`, committed consensus params are the runtime source of truth. An
existing network must use its independently verified canonical genesis.

Create and fund a development validator, then assemble and validate genesis:

```bash
docker_gurud keys add validator \
  --algo eth_secp256k1 \
  --keyring-backend test

VALIDATOR_ADDRESS="$(
  docker_gurud keys show validator \
    --address \
    --keyring-backend test
)"

docker_gurud genesis add-genesis-account \
  "$VALIDATOR_ADDRESS" \
  1000000000000000000000agxn

docker_gurud genesis add-genesis-account \
  "$CONSTITUTION_BASE_ADDRESS" \
  100000000000000000000agxn

docker_gurud genesis add-genesis-account \
  "$CONSTITUTION_MODERATOR_ADDRESS" \
  100000000000000000000agxn

docker_gurud genesis gentx \
  validator \
  1000000000000000000agxn \
  --chain-id "$GURU_CHAIN_ID" \
  --gas 200000 \
  --gas-prices 630000000000agxn \
  --keyring-backend test

docker_gurud genesis collect-gentxs
docker_gurud genesis validate
docker_gurud config get app mempool.max-txs
```

The `630000000000agxn` gentx gas price matches the default genesis FeeMarket
minimum. A lower fee can pass file-level genesis validation but fail when the
application executes the gentx during `InitChain`. If you intentionally change
the genesis FeeMarket minimum, update the gentx price accordingly.

The final command must print `-1`. Guru uses the SDK `NoOpMempool`; CometBFT
continues to own transaction storage and gossip.

The node image does not include `oracled`. An unavailable sidecar does not stop
ordinary consensus, but a node that will not participate in Oracle vote
extensions should make that intent explicit before startup:

```bash
docker_gurud config set app oracle.enabled false
```

See the [Oracle sidecar guide](../oracle/README.md) before enabling Oracle
participation on a validator.

## Start and operate the node

The base Compose model publishes only P2P (`26656`):

```bash
docker compose up --detach --no-build
```

For local development, opt in to host-loopback RPC and API port mappings:

```bash
docker compose -f compose.yaml -f compose.local.yaml up --detach --no-build
```

`compose.local.yaml` makes CometBFT RPC and gRPC listen inside the container and
maps them to host `127.0.0.1`. REST and JSON-RPC remain disabled unless these
non-secret `.env` values are changed:

```dotenv
GURU_API_ENABLE=true
GURU_JSON_RPC_ENABLE=true
```

REST also needs a container-reachable address in `app.toml`, set once before
startup:

```bash
docker_gurud config set app api.address tcp://0.0.0.0:1317
```

Common operations are:

```bash
docker compose -f compose.yaml -f compose.local.yaml ps
docker compose -f compose.yaml -f compose.local.yaml logs --follow gurud
docker compose -f compose.yaml -f compose.local.yaml exec -T gurud \
  gurud status --node tcp://127.0.0.1:26657

docker compose -f compose.yaml -f compose.local.yaml stop --timeout 120
docker compose -f compose.yaml -f compose.local.yaml start
docker compose -f compose.yaml -f compose.local.yaml restart --timeout 120
```

`docker compose down` removes the container and network but retains the named
volume. `docker compose down --volumes` deletes the node database, validator
state, node identity, keyring, and genesis; use it only for deliberately
discarding a local network.

## Join an existing testnet

Set the coordinated network values in `.env` before initialization:

```dotenv
GURU_CHAIN_ID=guru_testnet-1
GURU_EVM_CHAIN_ID=9631
GURU_MINIMUM_GAS_PRICES=630000000000agxn
GURU_P2P_SEEDS=node-id@seed.example.org:26656
GURU_PERSISTENT_PEERS=node-id@peer.example.org:26656
```

Run `gurud init` as above to create the node and configuration files, using the
network's real Constitution addresses. The downloaded, verified genesis—not the
temporary init genesis—is authoritative for an existing network. Download it
through the testnet's trusted distribution channel and verify its advertised
SHA-256 checksum on the host:

```bash
EXPECTED_GENESIS_SHA256=replace_with_the_advertised_checksum

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_GENESIS_SHA256="$(sha256sum genesis.json | awk '{print $1}')"
else
  ACTUAL_GENESIS_SHA256="$(shasum -a 256 genesis.json | awk '{print $1}')"
fi

test "$ACTUAL_GENESIS_SHA256" = "$EXPECTED_GENESIS_SHA256"
```

Install and validate the verified file in the named volume:

```bash
docker compose run --rm --no-deps -T \
  --entrypoint /bin/sh \
  --volume "$PWD/genesis.json:/tmp/genesis.json:ro" \
  gurud \
  -c 'cp /tmp/genesis.json "$HOME/.gurud/config/genesis.json"'

docker_gurud genesis validate
```

Start with the base Compose model for a validator or peer whose RPC services
must not be reachable from the host. Use the local override only when
loopback-only host access is required.

## Ports and security defaults

| Port | Service | Base Compose | Local override |
|---|---|---|---|
| `26656` | CometBFT P2P | Host-published | Host-published |
| `26657` | CometBFT RPC | Not published | `127.0.0.1` |
| `9090` | gRPC | Not published | `127.0.0.1` |
| `1317` | REST | Not published | `127.0.0.1`, disabled by default |
| `8545` | JSON-RPC HTTP | Not published | `127.0.0.1`, disabled by default |
| `8546` | JSON-RPC WebSocket | Not published | `127.0.0.1`, disabled by default |

The Dockerfile's `EXPOSE` entries are image metadata; they do not publish a
port. Changing a bind IP to `0.0.0.0` can make an administrative API reachable
from other machines and requires separate authentication, firewall, proxy,
rate-limit, and TLS decisions.

The long-running container:

- runs as fixed UID/GID `1025:1025`;
- drops all Linux capabilities and prevents privilege escalation;
- uses a read-only root filesystem with a bounded `/tmp` tmpfs;
- writes only to `/var/lib/guru/.gurud` on the named volume; and
- receives `SIGTERM` directly as PID 1 and has a two-minute stop grace period.

Do not use `--privileged`, run the node as root, place mnemonics in `.env`, pass
secrets as Docker build arguments, or bake genesis/private keys into the image.
For a bind mount instead of a named volume, make the host directory writable by
UID/GID `1025:1025` without making it world-writable.

## State backup, upgrades, and rollback

Stop the node gracefully before a filesystem-level backup. A complete backup
must keep `config/`, `data/`, validator signing state, node identity, and any
local keyring consistent. Treat the backup as secret key material. Test restore
into a separate volume before relying on it.

For a local image upgrade:

1. Back up and verify the volume.
2. Build the new checkout under a distinct image tag, for example
   `make docker-build DOCKER_IMAGE=guru-node:testnet-next`.
3. Set `GURU_DOCKER_IMAGE=guru-node:testnet-next` in `.env`.
4. Run `docker compose up --detach --no-build`; Compose replaces the container
   while retaining `guru-data`.
5. Confirm health, block height, peer connectivity, and application logs.

Rollback means restoring both a compatible previous image and, when an upgrade
migrated state, the matching pre-upgrade volume backup. Never assume a database
written by a newer binary is backward-compatible.

## Automated local integration check

Run:

```bash
make docker-integration
```

The target rebuilds the local image first. The check then uses a uniquely named
temporary volume. It generates development
keys without printing their mnemonics, initializes and validates genesis,
starts a read-only/non-root container, waits for blocks, stops it gracefully,
restarts it, and verifies that the node ID and block history persist. Only the
temporary resources carrying the `guru-docker-smoke-*` prefix are removed.
