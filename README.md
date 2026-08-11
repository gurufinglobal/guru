# Guru

Guru is a Cosmos SDK v0.53.6 validator application with a persistent Cosmos EVM v0.6.1 runtime.

`gurud` provides node initialization, genesis and key management, validator startup and restart, Cosmos queries and transactions, and Ethereum JSON-RPC/WebSocket endpoints. The runtime uses the upstream Cosmos EVM CheckTx path, CometBFT's mempool for transaction storage and gossip, and the SDK's NoOp app-side mempool with its default proposal handlers.

## Repository layout

```text
.
├── app/                       # state machine, keepers, modules, and server boundary
├── cmd/gurud/                 # operator and client command tree
├── config/                    # immutable chain identity
├── scripts/localnet-smoke.sh  # four-validator runtime acceptance harness
├── Makefile
├── go.mod
└── go.sum
```

## Chain identity

The canonical identity values are defined in `config/identity.go`.

| Property | Value |
|---|---|
| Binary and BaseApp name | `gurud` |
| Default home | `~/.gurud` |
| CometBFT chain ID | `guru_631-1` |
| Account prefix | `guru` |
| Validator prefix | `guruvaloper` |
| Consensus prefix | `guruvalcons` |
| Base denomination | `agxn` |
| Display denomination | `gxn` |
| Denomination exponent | `18` |
| EIP-155 chain ID | `631` (`0x277`) |
| BIP-44 coin type | `60` |

Genesis creation and node startup enforce `guru_631-1`. Ethereum signing and execution use EIP-155 chain ID `631`.

## Runtime scope

This runtime creates and loads databases produced by the current Guru application layout. Migration from earlier application layouts is not provided.

The binary installs the complete Cosmos EVM v0.6.1 `DefaultStaticPrecompiles` implementation set: Prague, P256, Bech32, staking, distribution, ICS20, bank, governance, and slashing. The generated default genesis keeps `active_static_precompiles` empty, so only the always-on Prague contracts are callable initially. Governance can subsequently add or remove installed extension addresses through the upstream VM parameter update path. The v0.6.1 vesting address (`0x...0803`) is declared by the VM types package but is not implemented by `DefaultStaticPrecompiles`; it must remain inactive until a binary upgrade installs its implementation. Upstream `MsgUpdateParams` does not verify the in-memory implementation registry.

Deployed external ERC-20 contracts can be registered and converted to and from their bank representations through the upstream Cosmos EVM ERC20 module. The generated default genesis starts without token pairs, allowances, or native/dynamic ERC-20 precompile state; general genesis validation also accepts legitimate exported runtime state. Preinstalls are EVM bytecode system contracts and are separate from the native static precompile registry. The default genesis installs only the upstream EIP-2935 history-storage contract so block-hash history is recorded from the first block. Other preinstalls remain absent and governance may add them through upstream `MsgRegisterPreinstalls`; upstream v0.6.1 does not provide a remove, replace, or disable message.

IBC core, the Tendermint light client, the ICS-20 transfer keeper/module, and the v1/v2 transfer routes are wired so the ICS20 precompile has complete dependencies if governance activates it. The generated default genesis contains no external IBC clients or channels; using ICS-20 still requires the usual counterparty, client, connection, and channel setup.

The custom Cosmos EVM transaction indexer is disabled by default. Ethereum transaction and receipt lookup uses Cosmos EVM's fallback over the CometBFT KV transaction index.

CometBFT owns transaction storage and gossip; Guru does not enable the
experimental app-side EVM pool. Chain-state RPCs, current-nonce transaction
submission, receipts, and CometBFT-backed pending queries remain available.
Future-nonce queuing, same-nonce replacement, price-bump policy, and populated
`txpool_*` results are not provided.

Changing `mempool.max-txs` in `app.toml` does not enable an app-side EVM pool
in this runtime profile; that setting is unsupported while the application
uses the SDK NoOp mempool.

## Build

Go `1.23.8` is required.

```bash
make build VERSION=<version> COMMIT=<git-commit>
./build/gurud version --long --output json
```

The binary is written to `build/gurud`.

## Initialize a validator

The following creates a development validator under a repository-local home. The `test` keyring stores private keys unencrypted and must not be used for production validators.

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

Start the node in the foreground:

```bash
./build/gurud start \
  --chain-id guru_631-1 \
  --home "$GURU_HOME"
```

Stopping the process normally flushes persistent application and CometBFT state. Starting it again with the same home loads the latest committed state.

## Export and import

Genesis validation applies module schemas and the chain's cross-module runtime
invariants. State created after `InitChain` remains importable through a normal
runtime export.

Stop the node before exporting its database, then write and validate a complete
normal-height genesis document:

```bash
./build/gurud export \
  --home "$GURU_HOME" \
  --output-document exported-genesis.json

./build/gurud genesis validate exported-genesis.json \
  --home "$GURU_HOME"
```

Use the exported document as `config/genesis.json` in newly initialized homes.
The current runtime preserves the exported height and validator set; it does
not implement `--for-zero-height` rewriting or a jail allowlist.

## Status, queries, and Cosmos transactions

The default CometBFT RPC endpoint is `tcp://127.0.0.1:26657`.

```bash
./build/gurud status \
  --node tcp://127.0.0.1:26657 \
  --home "$GURU_HOME"

./build/gurud query bank balance \
  "$VALIDATOR_ADDRESS" \
  agxn \
  --node tcp://127.0.0.1:26657 \
  --home "$GURU_HOME"
```

A signed bank transfer can be submitted with:

```bash
RECIPIENT="guru..."

./build/gurud tx bank send \
  validator \
  "$RECIPIENT" \
  1000000000000000000agxn \
  --node tcp://127.0.0.1:26657 \
  --chain-id guru_631-1 \
  --gas 200000 \
  --gas-prices 2000000000agxn \
  --keyring-backend test \
  --home "$GURU_HOME" \
  --yes
```

The EVM CLI accepts a signed EIP-155 transaction for Ethereum chain ID `631`
while retaining the Cosmos chain ID `guru_631-1` for the outer command:

```bash
SIGNED_ETHEREUM_TX="0x..."

./build/gurud tx evm raw "$SIGNED_ETHEREUM_TX" \
  --node tcp://127.0.0.1:26657 \
  --chain-id guru_631-1 \
  --home "$GURU_HOME" \
  --yes
```

`tx evm send` is also available as a native bank-transfer convenience command
that accepts either Guru bech32 or Ethereum hex account addresses.

Online Cosmos transaction commands also support `--sign-mode textual`. The
client resolves denomination metadata through the configured gRPC endpoint, or
through the CometBFT query fallback when no gRPC endpoint is selected. Textual
signing is intentionally unavailable with `--offline` because metadata cannot
be resolved safely.

## Ethereum JSON-RPC and WebSocket

The generated configuration enables the Ethereum endpoints on loopback addresses:

| Interface | Endpoint |
|---|---|
| JSON-RPC HTTP | `http://127.0.0.1:8545` |
| JSON-RPC WebSocket | `ws://127.0.0.1:8546` |

Verify the EIP-155 chain ID:

```bash
curl --silent --show-error \
  --header 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
  http://127.0.0.1:8545
```

The expected result is `0x277`. The `eth` namespace supports block, balance, and nonce queries, signed EIP-155 transaction submission through `eth_sendRawTransaction`, and receipt lookup through `eth_getTransactionReceipt`.

The default bindings are intentionally loopback-only. Cosmos EVM v0.6.1's
`eth` namespace includes signing methods that are not covered by the insecure
unlock setting, so do not expose either endpoint outside the host without
explicit namespace, keyring, authentication, and network-access controls.

## Four-validator acceptance

The smoke harness builds the binary, creates four isolated node homes, starts a four-validator network, checks consensus and state agreement, exercises Cosmos and current-nonce Ethereum transactions, verifies EVM revert semantics, restarts one node, checks catch-up, exports the stopped runtime databases, and imports the highest state into a second four-validator network before terminating every process.

The run records a representative wiring smoke matrix across the configured
keepers. It checks required gRPC service presence and selected semantic CLI
queries while the Comet query endpoint is deliberately unreachable. Signed
state transitions cover bank, staking, distribution, governance, authz,
feegrant, EVM, and ERC20. Other keeper boundaries are checked through
applicable query, lifecycle, authority, or empty-state paths; this is not an
invocation of every Query or Msg RPC.

Run this harness only through the repository's configured remote test runner.
The runner's machine-local path belongs in internal developer configuration,
not in this public document; do not invoke the harness directly on a developer
workstation.
