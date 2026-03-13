# Oracle Daemon (`oracled`)

Oracle daemon that listens for on-chain oracle requests, fetches off-chain data from configured providers, aggregates it, signs the result with the configured key, and submits `MsgSubmitOracleReport` back to the Guru chain.

- Event listener subscribes to new blocks for oracle task IDs and min gas price updates, then pushes work into the pipeline.
- Aggregator queries all providers for the task category and picks the median value.
- Submitter signs with the configured keyring entry and broadcasts via the chain RPC endpoint. Gas price is refreshed whenever the chain publishes a new minimum gas price.

## Build

From the repo root:

```bash
go build ./cmd/oracled
./oracled --help
```

You can also run without building a binary:

```bash
go run ./cmd/oracled --help
```

## Home and config layout

- Global flag: `--home` (default: your OS home). Runtime files live under `<home>/.oracled`.
- Config path: `<home>/.oracled/config.toml`.
- Default config written by `oracled init`:

```toml
[chain]
chain_id = "guru_631-1"
endpoint = "http://localhost:26657"

[keyring]
name = "oracle_feeder"
backend = "test"
passphrase = "password"

[gas]
limit = 70000
adjustment = 1.5
denom = "agxn"
```

## Commands

### `oracled init`

Initialize the home directory and write a default config if none exists.

```bash
./oracled init --home /path/to/base  # creates /path/to/base/.oracled/config.toml
```

- Idempotent: if `config.toml` already exists it is left untouched.
- Edit the generated config to point at your chain endpoint, keyring backend, key name, and gas settings.

### Prepare the keyring

`oracled start` expects the keyring entry named in `keyring.name` to exist under `<home>/.oracled` for the chosen backend (e.g., `<home>/.oracled/keyring-test` when backend is `test`).

Example (using Guru CLI keyring tooling):

```bash
gurud keys add oracle_feeder \
  --home /path/to/base/.oracled \
  --keyring-backend test
```

### `oracled start`

Start the daemon after configuration and keyring setup:

```bash
./oracled start --home /path/to/base
```

What it does:

- Loads `<home>/.oracled/config.toml` and verifies the keyring directory exists for file/test backends.
- Connects to the chain RPC/WebSocket endpoint, queries supported oracle categories, and builds a provider registry (Coinbase by default).
- Launches the pipeline:
  - Listener subscribes to oracle task IDs and min gas price updates.
  - Aggregator fetches from all providers for the task category and selects the median.
  - Submitter signs with the configured key and broadcasts the report; gas price is refreshed when the chain updates the minimum.
- Handles SIGINT/SIGTERM for graceful shutdown and restarts components if the Comet RPC client becomes unhealthy.

## Adding a provider

Providers implement `provider.Provider` and must return values the chain accepts (decimal strings accepted by `oracletypes.ParseOracleDecimal`).

1) Create your provider (example skeleton):

```go
// oracle/provider/myprovider.go
type MyProvider struct { client *http.Client }

func NewMyProvider(client *http.Client) *MyProvider { return &MyProvider{client: client} }
func (p *MyProvider) ID() string                     { return "myprovider" }
func (p *MyProvider) Categories() []int32            { return []int32{1} } // match chain categories
func (p *MyProvider) SetHTTPClient(c *http.Client)   { if c != nil { p.client = c } }
func (p *MyProvider) Fetch(ctx context.Context, symbol string) (string, error) {
    // Return a chain-acceptable decimal string; honor context for timeouts.
    return "123.45", nil
}
```

2) Register it in the daemon (ensure at least one provider per category):

```go
httpClient := &http.Client{Timeout: 30 * time.Second}
coinbase := provider.NewCoinbaseProvider(httpClient)
myPv := provider.NewMyProvider(httpClient)

registry, err := provider.New(logger, categories.Categories,
    coinbase,
    myPv, // add your provider here
)
daemon.providers = []provider.Provider{coinbase, myPv} // keep track for restarts
```

Notes:
- Registry will ignore unknown categories and errors if any chain category has zero providers; max 10 providers per category.
- Implement `SetHTTPClient` so the daemon can swap clients on restart.
- Ensure `Fetch` honors context and returns values accepted by `oracletypes.ParseOracleDecimal`; invalid formats are rejected during aggregation.


