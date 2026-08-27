# Testnet validator mempool profile

This profile covers the rendered mempool files supplied to a Testnet
validator. Network capacity and timeout choices remain local to each node.

## Policy

| Setting | Classification | Result |
|---|---|---|
| `app.toml:mempool.max-txs=-1` | required | missing, wrong type, or mismatch fails |
| `config.toml:mempool.type="flood"` | required | missing, wrong type, or mismatch fails |
| node binary `server_name="gurud"` | required | mismatch fails |
| `config.toml:mempool.recheck=true` | recommended | `false` warns and exits successfully; missing or wrong type fails |
| `broadcast`, `keep-invalid-txs-in-cache`, `wal_dir` | node-local | typed values are recorded without imposing global values |
| `size`, `max_txs_bytes`, `cache_size`, `max_tx_bytes`, `max_batch_bytes` | node-local | non-negative integer values are recorded without imposing global values |
| `recheck_timeout`, experimental gossip connection limits | node-local | typed values are recorded without imposing global values |

`app.toml` and CometBFT's `config.toml` contain different `[mempool]`
tables. The rendered values are:

```toml
# app.toml
[mempool]
max-txs = -1
```

```toml
# config.toml
[mempool]
type = "flood"
recheck = true
```

The application separately uses BaseApp's `NoOpMempool`; `max-txs=-1` is a
deployment-profile guard, not an application-side mempool activation switch.

With `recheck=true`, CometBFT submits remaining transactions to `ReCheckTx`
after a commit and can evict transactions whose recheck result no longer meets
a raised MGP. Transactions removed while MGP is high are not restored
automatically if MGP later falls. Disabling recheck does not bypass the
deterministic `FinalizeBlock` ante check, but can waste mempool and proposal
capacity.

## Preflight command

Run the standalone Bash script against a rendered home and its deployment
binary:

```bash
bash scripts/verify-validator-mempool-config.sh \
  --home /var/lib/guru \
  --validator-id validator-seoul-01 \
  --node-binary /opt/guru/bin/gurud \
  --output /var/lib/guru-evidence/validator-seoul-01.json
```

Bash, `jq`, and either `sha256sum` or `shasum` are required. The script uses
the supplied binary's `config view` command for both TOML files and its
`version --long --output json` command.

- Exit `0`: required and structural checks pass, including a `recheck=false`
  recommendation warning.
- Exit `1`: parsed configuration has a policy or structural failure.
- Exit `2`: inputs, parser output, or evidence generation could not be handled.

The JSON status is `pass`, `pass_with_warnings`, or `fail`. When `--output` is
provided, the report is written with private permissions and an atomic rename;
otherwise it is written to standard output. Direct, symlink, and existing
hardlink collisions with input files are refused.

## Evidence boundary

Evidence includes validator ID, UTC time, chain ID, input paths,
SHA-256 hashes, binary version and commit, typed checks, node-local values, and
warnings. The script parses private snapshots with the matching binary snapshot,
uses `gurud`'s normal typed config loading, and rehashes the originals before
publication.

The scope is the rendered files and supplied binary only. The report does not
claim to observe a later process's CLI flags, service environment, or file
changes, and the script does not change `gurud` startup behavior.
