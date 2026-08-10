# Guru

Guru is a Cosmos SDK blockchain application with an in-process Cosmos EVM v0.6.1 state machine.

The application now composes persistent stores, core Cosmos SDK keepers, IBC core, FeeMarket, VM, module lifecycle handlers, and the Cosmos/Ethereum ante paths.

This revision is not yet an operator-facing validator node. The `start`, `init`, `status`, `query`, and `tx` commands, JSON-RPC, production EVM mempool/proposal handlers, IBC application routes, ERC-20 conversion, and stateful Cosmos precompiles remain unavailable. Unimplemented functionality is not represented by successful placeholder commands.

## Repository layout

```text
.
├── app/
│   ├── app.go
│   ├── encoding.go
│   ├── genesis.go
│   ├── handlers.go
│   ├── keepers.go
│   ├── module_accounts.go
│   ├── modules.go
│   ├── parameter_policy.go
│   ├── precompiles.go
│   └── store_keys.go
├── cmd/gurud/
│   ├── main.go
│   └── cmd/
│       └── root.go
├── config/
│   └── identity.go
├── .gitignore
├── LICENSE
├── Makefile
├── README.md
├── go.mod
└── go.sum
```

## Chain identity

The canonical identity values are defined in `config/identity.go`.

| Property | Value |
|---|---|
| Binary and BaseApp name | `gurud` |
| Default home | `~/.gurud` |
| Account prefix | `guru` |
| Validator prefix | `guruvaloper` |
| Consensus prefix | `guruvalcons` |
| Base denomination | `agxn` |
| Display denomination | `gxn` |
| Denomination exponent | `18` |
| EIP-155 chain ID | `631` |
| BIP-44 coin type | `60` |

The application enforces the CometBFT chain ID at construction and InitChain, and configures EIP-155 signing and execution with chain ID `631`.

## Runtime scope

Stage C supports a new chain and state created by this application. It does not migrate databases produced by earlier Guru application layouts; governance history must be empty, and IBC genesis must equal the upstream empty default.

Only the Prague Ethereum precompile registry is installed. Cosmos-specific stateful precompiles and governance preinstall registration are disabled, and bank sends to all reserved precompile addresses are blocked. Native staking, mint, governance deposit, and EVM denominations remain fixed to `agxn` across genesis and runtime parameter updates.

FeeMarket starts with EIP-1559 active and a one-`agxn`-per-gas floor, the smallest non-zero atto-GXN price. Runtime FeeMarket updates preserve the fee floor. IBC core is present because the upstream Cosmos EVM ante handler requires it; no packet application route is active.

## Build

Go `1.23.8` is required.

```bash
make build VERSION=<version> COMMIT=<git-commit>
```

The binary is written to `build/gurud`.

## Build verification

Run verification from the repository root using portable, repository-relative commands:

```bash
make build
./build/gurud version --long --output json
```

The only operational command currently exposed is `version`. Commands including `start`, `init`, `status`, `query`, and `tx` remain intentionally unavailable until the node, mempool, IBC application, and RPC paths are fully implemented.
