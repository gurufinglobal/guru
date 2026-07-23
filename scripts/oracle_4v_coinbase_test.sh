#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"
BIN="${BIN:-$REPO/build/gurud}"
ORACLED="${ORACLED:-$REPO/build/oracled}"
CHAIN_ID="${CHAIN_ID:-guru_631}"
BASE="${BASE:-/private/tmp/guru-oracle-4v-$(date +%Y%m%d%H%M%S)}"
BASE_ADDR="${BASE_ADDR:-guru1yysjzgfpyysjzgfpyysjzgfpyysjzgfp8756qs}"
MOD_ADDR="${MOD_ADDR:-guru1yg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3zk6jltx}"
BALANCE="${BALANCE:-100000000000000000000agxn}"
STAKE="${STAKE:-10000000000000000000agxn}"
GENTX_FEE="${GENTX_FEE:-10000000000000000000agxn}"
PRICE_URL="${PRICE_URL:-https://api.coinbase.com/v2/prices/BTC-USD/spot}"

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

if [[ ! -x "$BIN" ]]; then
	echo "missing gurud binary: $BIN" >&2
	exit 1
fi
if [[ ! -x "$ORACLED" ]]; then
	echo "missing oracled binary: $ORACLED" >&2
	exit 1
fi

echo "base_dir=$BASE"

PORT_BASE="${PORT_BASE:-$((35000 + (RANDOM % 2000)))}"
declare -a HOMES RPC_PORTS P2P_PORTS PROXY_PORTS PPROF_PORTS GRPC_PORTS API_PORTS GRPC_WEB_PORTS JSONRPC_PORTS JSONRPC_WS_PORTS ORACLE_HOMES ORACLE_SOCKETS

for i in 1 2 3 4; do
	idx=$((i - 1))
	HOMES[$i]="$BASE/node$i"
	ORACLE_HOMES[$i]="$BASE/oracled$i"
	ORACLE_SOCKETS[$i]="$BASE/oracle$i.sock"
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
done

GENESIS_HOME="${HOMES[1]}"
for i in 1 2 3 4; do
	addr="$("$BIN" keys show "val$i" -a --keyring-backend test --home "${HOMES[$i]}")"
	"$BIN" genesis add-genesis-account "$addr" "$BALANCE" --home "$GENESIS_HOME" >"$BASE/logs/add-genesis-account-$i.log" 2>&1
done

genesis_path="$GENESIS_HOME/config/genesis.json"
jq '
	.app_state.oracle.params.min_validators = 4 |
	.app_state.oracle.params.min_sources = 1 |
	.app_state.oracle.params.history_limit = 100 |
	.app_state.oracle.tasks = [
		{
			"symbol": "BTC/USD",
			"value_type": 1,
			"enabled": true,
			"submission_interval": 1
		}
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

write_oracled_config() {
	local i="$1"
	local home="${ORACLE_HOMES[$i]}"
	mkdir -p "$home/config"
	cat >"$home/config/oracled.toml" <<EOF
socket = "${ORACLE_SOCKETS[$i]}"
request_timeout = "3s"
source_timeout = "2s"
node_grpc = "127.0.0.1:${GRPC_PORTS[$i]}"
node_query_timeout = "3s"

[[sources]]
name = "coinbase-btc-usd"
symbol = "BTC/USD"
value_type = "NUMERIC"
url = "$PRICE_URL"
response_path = "data.amount"
timeout = "2s"
interval = "1s"
EOF
}

for i in 1 2 3 4; do
	write_oracled_config "$i"
done

for i in 1 2 3 4; do
	"$BIN" start --home "${HOMES[$i]}" --log_level info >"$BASE/logs/gurud-$i.log" 2>&1 &
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

echo "querying oracle latest value..."
deadline=$((SECONDS + 120))
latest_file="$BASE/latest-value.json"
while true; do
	if "$BIN" query oracle latest-values \
		--node "tcp://127.0.0.1:${RPC_PORTS[1]}" \
		--home "${HOMES[1]}" \
		--chain-id "$CHAIN_ID" \
		--output json >"$latest_file" 2>"$BASE/logs/query-latest.err"; then
		value="$(jq -r '.values[]? | select(.symbol == "BTC/USD") | .value' "$latest_file")"
		if [[ -n "$value" && "$value" != "null" ]]; then
			height="$(jq -r '.values[]? | select(.symbol == "BTC/USD") | .block_height' "$latest_file")"
			block_time="$(jq -r '.values[]? | select(.symbol == "BTC/USD") | .block_time_unix' "$latest_file")"
			echo "oracle_value=$value"
			echo "oracle_height=$height"
			echo "oracle_block_time_unix=$block_time"
			echo "latest_json=$latest_file"
			exit 0
		fi
	fi
	if (( SECONDS > deadline )); then
		echo "oracle latest value did not appear before timeout" >&2
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
