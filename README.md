# Guru

Guru is a Cosmos SDK blockchain application. The current revision establishes the application bootstrap, chain identity, encoding boundary, and `gurud` command entry point.

This revision is not yet a runnable validator node. Persistent stores, keepers, the module manager, transaction handlers, ABCI lifecycle wiring, EVM execution, JSON-RPC, and the `start` command remain intentionally unavailable. Unimplemented functionality is not represented by successful placeholder commands.

## Repository layout

```text
.
├── app/
│   ├── app.go
│   ├── encoding.go
│   ├── genesis.go
│   └── options.go
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

The EIP-155 and local CometBFT chain identifiers are identity declarations only at this stage; the runtime does not yet enforce a network configuration.

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

The only operational command currently exposed is `version`. Commands including `start`, `init`, `status`, `query`, and `tx` are intentionally unavailable until their runtime paths are fully implemented.
