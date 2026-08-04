#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"
BIN="${BIN:-$REPO/build/gurud}"
ORACLED="${ORACLED:-$REPO/build/oracled}"
CHAIN_ID="${CHAIN_ID:-guru_631}"
BASE="${BASE:-/private/tmp/guru-oracle-4v-live-$(date +%Y%m%d%H%M%S)}"
BASE_ADDR="${BASE_ADDR:-}"
MOD_ADDR="${MOD_ADDR:-}"
BALANCE="${BALANCE:-100000000000000000000agxn}"
TX_BALANCE="${TX_BALANCE:-100000000000000000000agxn}"
STAKE="${STAKE:-10000000000000000000agxn}"
GENTX_FEE="${GENTX_FEE:-10000000000000000000agxn}"
COINBASE_PRICE_BASE_URL="${COINBASE_PRICE_BASE_URL:-https://api.coinbase.com/v2/prices}"
# This local harness verifies Coinbase payload compatibility, not provider independence.
ORACLE_SYMBOLS=("BTC/USD" "ETH/USD" "SOL/USD")
COINBASE_PRODUCTS=("BTC-USD" "ETH-USD" "SOL-USD")
COINBASE_PRICE_KINDS=("spot" "buy" "sell")

mkdir -p "$BASE/logs"

NODE_PIDS=()
ORACLE_PIDS=()

cleanup() {
	for pid in "${ORACLE_PIDS[@]:-}"; do
		kill "$pid" >/dev/null 2>&1 || true
	done
	for pid in "${NODE_PIDS[@]:-}"; do
		kill "$pid" >/dev/null 2>&1 || true
	done
	wait >/dev/null 2>&1 || true
}
trap cleanup EXIT

require_tool() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required tool: $1" >&2
		exit 1
	fi
}

require_tool jq
require_tool perl
require_tool python3
require_tool curl

if [[ ! -x "$BIN" ]]; then
	echo "missing gurud binary: $BIN" >&2
	exit 1
fi
if [[ ! -x "$ORACLED" ]]; then
	echo "missing oracled binary: $ORACLED" >&2
	exit 1
fi

preflight_sources() {
	if [[ "$COINBASE_PRICE_BASE_URL" != https://* ]]; then
		echo "Coinbase price base URL must use HTTPS: $COINBASE_PRICE_BASE_URL" >&2
		exit 1
	fi

	local index symbol product expected_base expected_currency kind url response amount
	for index in "${!ORACLE_SYMBOLS[@]}"; do
		symbol="${ORACLE_SYMBOLS[$index]}"
		product="${COINBASE_PRODUCTS[$index]}"
		expected_base="${product%-*}"
		expected_currency="${product#*-}"
		for kind in "${COINBASE_PRICE_KINDS[@]}"; do
			url="${COINBASE_PRICE_BASE_URL%/}/$product/$kind"
			if ! response="$(curl --fail-with-body --silent --show-error \
				--connect-timeout 3 --max-time 10 --retry 2 --retry-all-errors \
				--header 'Accept: application/json' \
				--header 'User-Agent: guru-oracle-local-acceptance/1' \
				"$url")"; then
				echo "oracle source preflight failed: symbol=$symbol source=coinbase-$kind url=$url" >&2
				exit 1
			fi
			if ! amount="$(printf '%s' "$response" | jq -er \
				--arg base "$expected_base" \
				--arg currency "$expected_currency" \
				'.data | select(.base == $base and .currency == $currency) | .amount | select(type == "string" and (tonumber > 0))')"; then
				echo "oracle source returned an unexpected payload: symbol=$symbol source=coinbase-$kind url=$url" >&2
				exit 1
			fi
			echo "source_ok symbol=$symbol source=coinbase-$kind amount=$amount url=$url"
		done
	done
}

preflight_sources

if [[ -z "$BASE_ADDR" || -z "$MOD_ADDR" ]]; then
	CONSTITUTION_HOME="$BASE/constitution"
	mkdir -p "$CONSTITUTION_HOME"
	if [[ -z "$BASE_ADDR" ]]; then
		"$BIN" keys add constitution-base --keyring-backend test --home "$CONSTITUTION_HOME" --output json >"$BASE/logs/key-constitution-base.json"
		BASE_ADDR="$(jq -r '.address' "$BASE/logs/key-constitution-base.json")"
	fi
	if [[ -z "$MOD_ADDR" ]]; then
		"$BIN" keys add constitution-moderator --keyring-backend test --home "$CONSTITUTION_HOME" --output json >"$BASE/logs/key-constitution-moderator.json"
		MOD_ADDR="$(jq -r '.address' "$BASE/logs/key-constitution-moderator.json")"
	fi
fi

echo "base_dir=$BASE"

PORT_BASE="${PORT_BASE:-$((37000 + (RANDOM % 2000)))}"
declare -a HOMES RPC_PORTS P2P_PORTS PROXY_PORTS PPROF_PORTS GRPC_PORTS API_PORTS GRPC_WEB_PORTS JSONRPC_PORTS JSONRPC_WS_PORTS ORACLE_HOMES ORACLE_SOCKETS TX_ADDRS

for i in 1 2 3 4; do
	idx=$((i - 1))
	HOMES[$i]="$BASE/node$i"
	ORACLE_HOMES[$i]="$BASE/oracled$i"
	ORACLE_SOCKETS[$i]="${ORACLE_HOMES[$i]}/run/oracle.sock"
	RPC_PORTS[$i]=$((PORT_BASE + idx * 20 + 1))
	P2P_PORTS[$i]=$((PORT_BASE + idx * 20 + 2))
	PROXY_PORTS[$i]=$((PORT_BASE + idx * 20 + 3))
	PPROF_PORTS[$i]=$((PORT_BASE + idx * 20 + 4))
	GRPC_PORTS[$i]=$((PORT_BASE + idx * 20 + 5))
	API_PORTS[$i]=$((PORT_BASE + idx * 20 + 6))
	GRPC_WEB_PORTS[$i]=$((PORT_BASE + idx * 20 + 7))
	JSONRPC_PORTS[$i]=$((PORT_BASE + idx * 20 + 8))
	JSONRPC_WS_PORTS[$i]=$((PORT_BASE + idx * 20 + 9))
done

for i in 1 2 3 4; do
	home="${HOMES[$i]}"
	"$BIN" init "val$i" \
		--chain-id "$CHAIN_ID" \
		--home "$home" \
		--constitution-base-address "$BASE_ADDR" \
		--constitution-moderator-address "$MOD_ADDR" \
		>"$BASE/logs/init-val$i.log" 2>&1
	"$BIN" keys add "val$i" --keyring-backend test --home "$home" --output json >"$BASE/logs/key-val$i.json"
	"$BIN" keys add "agent$i" --keyring-backend test --home "$home" --output json >"$BASE/logs/key-agent$i.json"
	TX_ADDRS[$i]="$(jq -r '.address' "$BASE/logs/key-agent$i.json")"
done

GENESIS_HOME="${HOMES[1]}"
for i in 1 2 3 4; do
	addr="$("$BIN" keys show "val$i" -a --keyring-backend test --home "${HOMES[$i]}")"
	"$BIN" genesis add-genesis-account "$addr" "$BALANCE" --home "$GENESIS_HOME" >"$BASE/logs/add-genesis-val-$i.log" 2>&1
	"$BIN" genesis add-genesis-account "${TX_ADDRS[$i]}" "$TX_BALANCE" --home "$GENESIS_HOME" >"$BASE/logs/add-genesis-agent-$i.log" 2>&1
done

genesis_path="$GENESIS_HOME/config/genesis.json"
jq '
	.app_state.oracle.params.min_validators = 4 |
	.app_state.oracle.params.min_sources = 3 |
	.app_state.oracle.params.history_limit = 100 |
	.app_state.oracle.tasks = [
		{
			"symbol": "BTC/USD",
			"value_type": 1,
			"enabled": true,
			"submission_interval": 1
		},
		{
			"symbol": "ETH/USD",
			"value_type": 1,
			"enabled": true,
			"submission_interval": 1
		},
		{
			"symbol": "SOL/USD",
			"value_type": 1,
			"enabled": true,
			"submission_interval": 1
		}
	] |
	.app_state.oracle.task_schedule = [
		{"symbol": "BTC/USD", "height": 3},
		{"symbol": "BTC/USD", "height": 4},
		{"symbol": "ETH/USD", "height": 3},
		{"symbol": "ETH/USD", "height": 4},
		{"symbol": "SOL/USD", "height": 3},
		{"symbol": "SOL/USD", "height": 4}
	] |
	.app_state.oracle.latest_values = [] |
	.app_state.oracle.history = []
' "$genesis_path" >"$BASE/genesis.oracle.json"
mv "$BASE/genesis.oracle.json" "$genesis_path"

for i in 2 3 4; do
	cp "$genesis_path" "${HOMES[$i]}/config/genesis.json"
done

for i in 1 2 3 4; do
	"$BIN" genesis gentx "val$i" "$STAKE" \
		--chain-id "$CHAIN_ID" \
		--keyring-backend test \
		--home "${HOMES[$i]}" \
		--fees "$GENTX_FEE" \
		>"$BASE/logs/gentx-$i.log" 2>&1
done

mkdir -p "$GENESIS_HOME/config/gentx"
for i in 2 3 4; do
	cp "${HOMES[$i]}"/config/gentx/*.json "$GENESIS_HOME/config/gentx/"
done
"$BIN" genesis collect-gentxs --home "$GENESIS_HOME" >"$BASE/logs/collect-gentxs.log" 2>&1
"$BIN" genesis validate "$GENESIS_HOME/config/genesis.json" --home "$GENESIS_HOME" >"$BASE/logs/validate-genesis.log" 2>&1

for i in 2 3 4; do
	cp "$GENESIS_HOME/config/genesis.json" "${HOMES[$i]}/config/genesis.json"
done

declare -a NODE_IDS PEERS
for i in 1 2 3 4; do
	NODE_IDS[$i]="$("$BIN" comet show-node-id --home "${HOMES[$i]}")"
	PEERS[$i]="${NODE_IDS[$i]}@127.0.0.1:${P2P_PORTS[$i]}"
done

configure_node() {
	local i="$1"
	local home="${HOMES[$i]}"
	local cfg="$home/config/config.toml"
	local app="$home/config/app.toml"
	local peers=""
	local peers_for_perl=""
	local j
	for j in 1 2 3 4; do
		if [[ "$j" != "$i" ]]; then
			if [[ -n "$peers" ]]; then
				peers+=","
			fi
			peers+="${PEERS[$j]}"
		fi
	done
	peers_for_perl="${peers//@/\\@}"

	perl -0pi -e "s#proxy_app = \"tcp://127\\.0\\.0\\.1:26658\"#proxy_app = \"tcp://127.0.0.1:${PROXY_PORTS[$i]}\"#g" "$cfg"
	perl -0pi -e "s#laddr = \"tcp://127\\.0\\.0\\.1:26657\"#laddr = \"tcp://127.0.0.1:${RPC_PORTS[$i]}\"#g" "$cfg"
	perl -0pi -e "s#pprof_laddr = \"localhost:6060\"#pprof_laddr = \"localhost:${PPROF_PORTS[$i]}\"#g" "$cfg"
	perl -0pi -e "s#laddr = \"tcp://0\\.0\\.0\\.0:26656\"#laddr = \"tcp://127.0.0.1:${P2P_PORTS[$i]}\"#g" "$cfg"
	perl -0pi -e "s#persistent_peers = \"\"#persistent_peers = \"$peers_for_perl\"#g" "$cfg"
	perl -0pi -e "s#addr_book_strict = true#addr_book_strict = false#g" "$cfg"
	perl -0pi -e "s#allow_duplicate_ip = false#allow_duplicate_ip = true#g" "$cfg"
	perl -0pi -e "s#timeout_commit = \"500ms\"#timeout_commit = \"2s\"#g" "$cfg"

	python3 - "$app" \
		"tcp://127.0.0.1:${API_PORTS[$i]}" \
		"127.0.0.1:${GRPC_PORTS[$i]}" \
		"127.0.0.1:${GRPC_WEB_PORTS[$i]}" \
		"127.0.0.1:${JSONRPC_PORTS[$i]}" \
		"127.0.0.1:${JSONRPC_WS_PORTS[$i]}" \
		"${ORACLE_SOCKETS[$i]}" <<'PY'
import sys

path, api_addr, grpc_addr, grpc_web_addr, jsonrpc_addr, jsonrpc_ws_addr, oracle_socket = sys.argv[1:]
section = ""
out = []

with open(path, "r", encoding="utf-8") as f:
    for line in f:
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            section = stripped.strip("[]")

        if section == "api":
            if stripped.startswith("enable ="):
                line = "enable = true\n"
            elif stripped.startswith("address ="):
                line = f'address = "{api_addr}"\n'
        elif section == "grpc":
            if stripped.startswith("enable ="):
                line = "enable = true\n"
            elif stripped.startswith("address ="):
                line = f'address = "{grpc_addr}"\n'
        elif section == "grpc-web":
            if stripped.startswith("address ="):
                line = f'address = "{grpc_web_addr}"\n'
        elif section == "json-rpc":
            if stripped.startswith("enable ="):
                line = "enable = true\n"
            elif stripped.startswith("address ="):
                line = f'address = "{jsonrpc_addr}"\n'
            elif stripped.startswith("ws-address ="):
                line = f'ws-address = "{jsonrpc_ws_addr}"\n'
        elif section == "oracle":
            if stripped.startswith("enabled ="):
                line = "enabled = true\n"
            elif stripped.startswith("sidecar_socket ="):
                line = f'sidecar_socket = "{oracle_socket}"\n'
            elif stripped.startswith("sidecar_timeout ="):
                line = 'sidecar_timeout = "3s"\n'

        out.append(line)

with open(path, "w", encoding="utf-8") as f:
    f.writelines(out)
PY
}

for i in 1 2 3 4; do
	configure_node "$i"
done

init_oracled_home() {
	local i="$1"
	local home="${ORACLE_HOMES[$i]}"
	local revision="coinbase-btc-eth-sol-usd-v1"
	local sources_path="$home/sources.toml"
	local sources_sha256

	"$ORACLED" init --home "$home" >"$BASE/logs/oracled-init-$i.log" 2>&1

	{
		printf 'schema_version = 1\npublication_revision = "%s"\n' "$revision"
		local index symbol product kind
		for index in "${!ORACLE_SYMBOLS[@]}"; do
			symbol="${ORACLE_SYMBOLS[$index]}"
			product="${COINBASE_PRODUCTS[$index]}"
			printf '\n[[feeds]]\nsymbol = "%s"\ninterval = "5s"\nstale_after = "30s"\n' "$symbol"
			for kind in "${COINBASE_PRICE_KINDS[@]}"; do
				printf '\n[[feeds.sources]]\nid = "coinbase-%s"\nurl = "%s/%s/%s"\njson_pointer = "/data/amount"\n' \
					"$kind" "${COINBASE_PRICE_BASE_URL%/}" "$product" "$kind"
			done
		done
	} >"$sources_path"

	sources_sha256="$(python3 - "$sources_path" <<'PY'
import hashlib
import pathlib
import sys

print(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest())
PY
)"

	cat >"$home/config.toml" <<EOF
schema_version = 1
publication_revision = "$revision"
sources_sha256 = "$sources_sha256"

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
EOF

	"$ORACLED" validate --home "$home" >"$BASE/logs/oracled-validate-$i.log" 2>&1
}

for i in 1 2 3 4; do
	init_oracled_home "$i"
done

for i in 1 2 3 4; do
	"$BIN" start \
		--home "${HOMES[$i]}" \
		--chain-id "$CHAIN_ID" \
		--minimum-gas-prices 0agxn \
		--log_level info \
		>"$BASE/logs/gurud-$i.log" 2>&1 &
	NODE_PIDS+=("$!")
done

wait_for_rpc() {
	local i="$1"
	local deadline=$((SECONDS + 60))
	until "$BIN" status --node "tcp://127.0.0.1:${RPC_PORTS[$i]}" >/dev/null 2>&1; do
		if (( SECONDS > deadline )); then
			echo "node $i RPC did not start; log follows" >&2
			tail -n 120 "$BASE/logs/gurud-$i.log" >&2 || true
			exit 1
		fi
		sleep 1
	done
}

for i in 1 2 3 4; do
	wait_for_rpc "$i"
done

wait_for_height() {
	local target="$1"
	local deadline=$((SECONDS + 90))
	while true; do
		height="$("$BIN" status --node "tcp://127.0.0.1:${RPC_PORTS[1]}" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"')"
		if [[ "$height" =~ ^[0-9]+$ ]] && (( height >= target )); then
			echo "height=$height"
			return
		fi
		if (( SECONDS > deadline )); then
			echo "chain did not reach height $target; node logs follow" >&2
			for i in 1 2 3 4; do
				echo "---- gurud-$i.log ----" >&2
				tail -n 80 "$BASE/logs/gurud-$i.log" >&2 || true
			done
			exit 1
		fi
		sleep 1
	done
}

wait_for_height 2

for i in 1 2 3 4; do
	"$ORACLED" start --home "${ORACLE_HOMES[$i]}" >"$BASE/logs/oracled-$i.log" 2>&1 &
	ORACLE_PIDS+=("$!")
done

sleep 3
for idx in "${!ORACLE_PIDS[@]}"; do
	pid="${ORACLE_PIDS[$idx]}"
	if ! kill -0 "$pid" >/dev/null 2>&1; then
		i=$((idx + 1))
		echo "oracled $i exited early; log follows" >&2
		cat "$BASE/logs/oracled-$i.log" >&2 || true
		exit 1
	fi
done

echo "querying oracle latest values..."
deadline=$((SECONDS + 120))
latest_file="$BASE/latest-value.json"
while true; do
	if "$BIN" query oracle latest-values \
		--node "tcp://127.0.0.1:${RPC_PORTS[1]}" \
		--home "${HOMES[1]}" \
		--chain-id "$CHAIN_ID" \
		--output json >"$latest_file" 2>"$BASE/logs/query-latest.err"; then
		all_values_ready=1
		for symbol in "${ORACLE_SYMBOLS[@]}"; do
			value="$(jq -r --arg symbol "$symbol" '[.values[]? | select(.symbol == $symbol) | .value][0] // empty' "$latest_file")"
			if [[ -z "$value" ]]; then
				all_values_ready=0
				break
			fi
		done
		if (( all_values_ready == 1 )); then
			for symbol in "${ORACLE_SYMBOLS[@]}"; do
				value="$(jq -r --arg symbol "$symbol" '.values[] | select(.symbol == $symbol) | .value' "$latest_file")"
				height="$(jq -r --arg symbol "$symbol" '.values[] | select(.symbol == $symbol) | .block_height' "$latest_file")"
				block_time="$(jq -r --arg symbol "$symbol" '.values[] | select(.symbol == $symbol) | .block_time_unix' "$latest_file")"
				echo "oracle_symbol=$symbol oracle_value=$value oracle_height=$height oracle_block_time_unix=$block_time"
			done
			break
		fi
	fi
	if (( SECONDS > deadline )); then
		echo "oracle latest values did not appear for every required symbol before timeout" >&2
		echo "last latest-values output:" >&2
		cat "$latest_file" >&2 || true
		echo "query error:" >&2
		cat "$BASE/logs/query-latest.err" >&2 || true
		for i in 1 2 3 4; do
			echo "---- oracled-$i.log ----" >&2
			tail -n 120 "$BASE/logs/oracled-$i.log" >&2 || true
			echo "---- gurud-$i.log oracle lines ----" >&2
			grep -i oracle "$BASE/logs/gurud-$i.log" | tail -n 120 >&2 || true
		done
		exit 1
	fi
	sleep 2
done

ready_env="$BASE/ready.env"
{
	echo "BASE=$BASE"
	echo "REPO=$REPO"
	echo "BIN=$BIN"
	echo "ORACLED=$ORACLED"
	echo "CHAIN_ID=$CHAIN_ID"
	for i in 1 2 3 4; do
		echo "HOME_$i=${HOMES[$i]}"
		echo "RPC_$i=${RPC_PORTS[$i]}"
		echo "GRPC_$i=${GRPC_PORTS[$i]}"
		echo "ORACLE_HOME_$i=${ORACLE_HOMES[$i]}"
		echo "ORACLE_SOCKET_$i=${ORACLE_SOCKETS[$i]}"
		echo "AGENT_NAME_$i=agent$i"
		echo "AGENT_ADDR_$i=${TX_ADDRS[$i]}"
	done
} >"$ready_env"

echo "ready_env=$ready_env"
echo "stop_file=$BASE/stop"
echo "testnet_ready=1"

while [[ ! -f "$BASE/stop" ]]; do
	sleep 1
done
