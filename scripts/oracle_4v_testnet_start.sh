#!/usr/bin/env bash
set -euo pipefail

umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"
BIN="${BIN:-$REPO/build/gurud}"
ORACLED="${ORACLED:-$REPO/build/oracled}"
CHAIN_ID="${CHAIN_ID:-guru_631-1}"
EVM_CHAIN_ID="${EVM_CHAIN_ID:-631}"
BOND_DENOM="${BOND_DENOM:-agxn}"
GENTX_GAS_PRICE="${GENTX_GAS_PRICE:-630000000000agxn}"
NODE_MINIMUM_GAS_PRICES="${NODE_MINIMUM_GAS_PRICES:-0agxn}"
BALANCE="${BALANCE:-100000000000000000000000000agxn}"
TX_BALANCE="${TX_BALANCE:-100000000000000000000agxn}"
STAKE="${STAKE:-1000000000000000000000agxn}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-180}"

ORACLE_SYMBOLS=("BTC/USD" "ETH/USD" "SOL/USD")
NODE_PIDS=()
ORACLE_PIDS=()
OWNED_PIDS=()
OWNED_PID_IDENTITIES=()
EXIT_AFTER_READY=0
BASE_WAS_PROVIDED=0
CLEANED_UP=0

usage() {
	cat <<'EOF'
Usage: scripts/oracle_4v_testnet_start.sh [--exit-after-ready]

Starts a local 4-validator Guru testnet with one oracled sidecar per validator.

Environment:
  BASE                      Empty base directory to use. Defaults to mktemp.
  PORT_BASE                 First port in the local port block. Defaults random.
  BIN                       gurud binary. Defaults build/gurud.
  ORACLED                   oracled binary. Defaults build/oracled.
  NODE_MINIMUM_GAS_PRICES   Runtime admission minimum. Defaults 0agxn.
  GENTX_GAS_PRICE           Genesis gentx gas price. Defaults 630000000000agxn.
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--exit-after-ready)
		EXIT_AFTER_READY=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

require_tool() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required tool: $1" >&2
		exit 1
	fi
}

require_file_mode() {
	local path="$1"
	local expected="$2"
	local actual
	actual="$(python3 - "$path" <<'PY'
import os
import stat
import sys

print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode))[2:])
PY
)"
	if [[ "$actual" != "$expected" ]]; then
		echo "expected $path mode $expected, got $actual" >&2
		exit 1
	fi
}

require_positive_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "$value" =~ ^[0-9]+$ ]] || [[ "$value" -le 0 ]]; then
		echo "$name must be a positive integer, got: $value" >&2
		exit 2
	fi
}

canonical_path() {
	python3 - "$1" <<'PY'
import os
import sys

print(os.path.realpath(sys.argv[1]))
PY
}

directory_is_empty() {
	python3 - "$1" <<'PY'
import os
import sys

with os.scandir(sys.argv[1]) as entries:
    sys.exit(1 if next(entries, None) is not None else 0)
PY
}

validate_runtime_inputs() {
	require_positive_integer WAIT_TIMEOUT "$WAIT_TIMEOUT"
	if [[ "$WAIT_TIMEOUT" -lt 30 || "$WAIT_TIMEOUT" -gt 3600 ]]; then
		echo "WAIT_TIMEOUT must be between 30 and 3600 seconds, got: $WAIT_TIMEOUT" >&2
		exit 2
	fi
	if [[ -n "${PORT_BASE:-}" ]]; then
		require_positive_integer PORT_BASE "$PORT_BASE"
		if [[ "$PORT_BASE" -lt 1024 || "$PORT_BASE" -gt 65466 ]]; then
			echo "PORT_BASE must be between 1024 and 65466, got: $PORT_BASE" >&2
			exit 2
		fi
	fi
}

port_in_use() {
	local port="$1"
	if command -v nc >/dev/null 2>&1; then
		nc -z 127.0.0.1 "$port" >/dev/null 2>&1
		return $?
	fi
	python3 - "$port" <<'PY'
import socket
import sys

sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.settimeout(0.2)
try:
    sys.exit(0 if sock.connect_ex(("127.0.0.1", int(sys.argv[1]))) == 0 else 1)
finally:
    sock.close()
PY
}

pid_alive() {
	kill -0 "$1" >/dev/null 2>&1
}

pid_is_zombie() {
	local state
	state="$(ps -p "$1" -o stat= 2>/dev/null)" || return 1
	[[ "$state" == Z* ]]
}

process_identity_matches() {
	local pid="$1"
	local expected="$2"
	local command
	command="$(ps -ww -p "$pid" -o command= 2>/dev/null)" || return 1
	[[ "$command" == *"$expected"* ]]
}

signal_owned_pid() {
	local idx="$1"
	local signal="$2"
	local pid="${OWNED_PIDS[$idx]}"
	local identity="${OWNED_PID_IDENTITIES[$idx]}"
	if ! pid_alive "$pid"; then
		return
	fi
	if pid_is_zombie "$pid"; then
		wait "$pid" >/dev/null 2>&1 || true
		return
	fi
	if ! process_identity_matches "$pid" "$identity"; then
		echo "cleanup_skip=pid_identity_mismatch pid=$pid expected=$identity" >&2
		return
	fi
	kill "$signal" "$pid" >/dev/null 2>&1 || true
}

cleanup() {
	local pid idx deadline alive
	if [[ "$CLEANED_UP" -eq 1 ]]; then
		return
	fi
	CLEANED_UP=1
	idx=0
	while [[ "$idx" -lt "${#OWNED_PIDS[@]}" ]]; do
		signal_owned_pid "$idx" -TERM
		idx=$((idx + 1))
	done
	deadline=$((SECONDS + 10))
	while true; do
		alive=0
		idx=0
		while [[ "$idx" -lt "${#OWNED_PIDS[@]}" ]]; do
			pid="${OWNED_PIDS[$idx]}"
			if pid_alive "$pid"; then
				if pid_is_zombie "$pid"; then
					wait "$pid" >/dev/null 2>&1 || true
				elif process_identity_matches "$pid" "${OWNED_PID_IDENTITIES[$idx]}"; then
					alive=1
				else
					echo "cleanup_skip=pid_identity_mismatch pid=$pid expected=${OWNED_PID_IDENTITIES[$idx]}" >&2
				fi
			else
				wait "$pid" >/dev/null 2>&1 || true
			fi
			idx=$((idx + 1))
		done
		if [[ "$alive" -eq 0 ]]; then
			echo "cleanup_result=terminated"
			return
		fi
		if (( SECONDS >= deadline )); then
			break
		fi
		sleep 1
	done
	idx=0
	while [[ "$idx" -lt "${#OWNED_PIDS[@]}" ]]; do
		pid="${OWNED_PIDS[$idx]}"
		if pid_alive "$pid"; then
			if pid_is_zombie "$pid"; then
				wait "$pid" >/dev/null 2>&1 || true
			elif process_identity_matches "$pid" "${OWNED_PID_IDENTITIES[$idx]}"; then
				kill -KILL "$pid" >/dev/null 2>&1 || true
				wait "$pid" >/dev/null 2>&1 || true
			else
				echo "cleanup_skip=pid_identity_mismatch pid=$pid expected=${OWNED_PID_IDENTITIES[$idx]}" >&2
			fi
		else
			wait "$pid" >/dev/null 2>&1 || true
		fi
		idx=$((idx + 1))
	done
	echo "cleanup_result=killed"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

require_tool jq
require_tool python3
require_tool curl

if [[ ! -x "$BIN" ]]; then
	echo "missing executable gurud binary: $BIN" >&2
	exit 1
fi
if [[ ! -x "$ORACLED" ]]; then
	echo "missing executable oracled binary: $ORACLED" >&2
	exit 1
fi

validate_runtime_inputs

if [[ -n "${BASE:-}" ]]; then
	BASE_WAS_PROVIDED=1
	case "$BASE" in
	/)
		echo "refusing unsafe BASE: $BASE" >&2
		exit 1
		;;
	esac
	if [[ -n "${HOME:-}" && "$BASE" == "$HOME" ]]; then
		echo "refusing unsafe BASE: $BASE" >&2
		exit 1
	fi
	if [[ -L "$BASE" ]]; then
		echo "refusing symlink BASE: $BASE" >&2
		exit 1
	fi
else
	BASE="$(mktemp -d "${ORACLE_TMP_ROOT:-${TMPDIR:-/tmp}}/guru-oracle-4v.XXXXXX")"
fi
BASE="$(canonical_path "$BASE")"
if [[ "$BASE_WAS_PROVIDED" -eq 0 && -z "${ORACLE_TMP_ROOT:-}" && "${#BASE}" -gt 55 ]]; then
	rmdir "$BASE"
	BASE="$(mktemp -d "/tmp/guru-oracle-4v.XXXXXX")"
	BASE="$(canonical_path "$BASE")"
fi
REPO_REAL="$(canonical_path "$REPO")"
case "$BASE" in
/ | "$REPO_REAL")
	echo "refusing unsafe BASE after canonicalization: $BASE" >&2
	exit 1
	;;
esac
if [[ -n "${HOME:-}" ]]; then
	HOME_REAL="$(canonical_path "$HOME")"
	if [[ "$BASE" == "$HOME_REAL" ]]; then
		echo "refusing unsafe BASE after canonicalization: $BASE" >&2
		exit 1
	fi
fi
if [[ "$BASE_WAS_PROVIDED" -eq 1 && -e "$BASE" ]]; then
	if [[ ! -d "$BASE" ]]; then
		echo "BASE exists and is not a directory: $BASE" >&2
		exit 1
	fi
	if ! directory_is_empty "$BASE"; then
		echo "BASE exists and is not empty: $BASE" >&2
		exit 1
	fi
fi
mkdir -p "$BASE/logs"
chmod 700 "$BASE"
require_file_mode "$BASE" "700"

echo "base_dir=$BASE"
echo "chain_id=$CHAIN_ID evm_chain_id=$EVM_CHAIN_ID"

declare -a HOMES RPC_PORTS P2P_PORTS PROXY_PORTS PPROF_PORTS GRPC_PORTS API_PORTS JSONRPC_PORTS JSONRPC_WS_PORTS ORACLE_HOMES ORACLE_SOCKETS TX_ADDRS NODE_IDS PEERS

choose_ports() {
	local attempt base i idx port ports collision explicit_port
	if [[ -n "${PORT_BASE:-}" ]]; then
		base="$PORT_BASE"
		explicit_port=1
		attempt=1
	else
		explicit_port=0
		attempt=1
	fi
	while [[ "$attempt" -le 20 ]]; do
		if [[ -z "${PORT_BASE:-}" ]]; then
			base=$((37000 + (RANDOM % 2000)))
		fi
		ports=""
		collision=0
		for i in 1 2 3 4; do
			idx=$((i - 1))
			RPC_PORTS[$i]=$((base + idx * 20 + 1))
			P2P_PORTS[$i]=$((base + idx * 20 + 2))
			PROXY_PORTS[$i]=$((base + idx * 20 + 3))
			PPROF_PORTS[$i]=$((base + idx * 20 + 4))
			GRPC_PORTS[$i]=$((base + idx * 20 + 5))
			API_PORTS[$i]=$((base + idx * 20 + 6))
			JSONRPC_PORTS[$i]=$((base + idx * 20 + 8))
			JSONRPC_WS_PORTS[$i]=$((base + idx * 20 + 9))
			for port in "${RPC_PORTS[$i]}" "${P2P_PORTS[$i]}" "${PROXY_PORTS[$i]}" "${PPROF_PORTS[$i]}" "${GRPC_PORTS[$i]}" "${API_PORTS[$i]}" "${JSONRPC_PORTS[$i]}" "${JSONRPC_WS_PORTS[$i]}"; do
				if printf '%s\n' "$ports" | grep -qx "$port"; then
					collision=1
				fi
				ports="${ports}${port}
"
				if port_in_use "$port"; then
					collision=1
				fi
			done
		done
		if [[ "$collision" -eq 0 ]]; then
			PORT_BASE="$base"
			echo "port_base=$PORT_BASE"
			return
		fi
		if [[ "$explicit_port" -eq 1 ]]; then
			echo "explicit PORT_BASE block is not fully available: $PORT_BASE" >&2
			exit 1
		fi
		attempt=$((attempt + 1))
	done
	echo "could not find an unused local port block" >&2
	exit 1
}

choose_ports

for i in 1 2 3 4; do
	HOMES[$i]="$BASE/node$i"
	ORACLE_HOMES[$i]="$BASE/oracled$i"
	ORACLE_SOCKETS[$i]="${ORACLE_HOMES[$i]}/run/oracle.sock"
done

add_key_address() {
	local name="$1"
	local home="$2"
	local output address
	output="$("$BIN" keys add "$name" --keyring-backend test --home "$home" --output json)"
	address="$(printf '%s' "$output" | jq -r '.address')"
	unset output
	printf '{"name":"%s","address":"%s"}\n' "$name" "$address" >"$BASE/logs/key-$name.json"
	printf '%s\n' "$address"
}

CONSTITUTION_HOME="$BASE/constitution"
mkdir -p "$CONSTITUTION_HOME"
BASE_ADDR="${BASE_ADDR:-$(add_key_address constitution-base "$CONSTITUTION_HOME")}"
MOD_ADDR="${MOD_ADDR:-$(add_key_address constitution-moderator "$CONSTITUTION_HOME")}"
echo "constitution_base_address=$BASE_ADDR"
echo "constitution_moderator_address=$MOD_ADDR"

edit_app_toml() {
	local app="$1"
	local api_addr="$2"
	local grpc_addr="$3"
	local jsonrpc_addr="$4"
	local jsonrpc_ws_addr="$5"
	local oracle_socket="$6"
	python3 - "$app" "$api_addr" "$grpc_addr" "$jsonrpc_addr" "$jsonrpc_ws_addr" "$oracle_socket" <<'PY'
import sys

path, api_addr, grpc_addr, jsonrpc_addr, jsonrpc_ws_addr, oracle_socket = sys.argv[1:]
section = ""
out = []
with open(path, "r", encoding="utf-8") as handle:
    for line in handle:
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
        elif section == "json-rpc":
            if stripped.startswith("enable ="):
                line = "enable = true\n"
            elif stripped.startswith("address ="):
                line = f'address = "{jsonrpc_addr}"\n'
            elif stripped.startswith("ws-address ="):
                line = f'ws-address = "{jsonrpc_ws_addr}"\n'
        elif section == "mempool":
            if stripped.startswith("max-txs ="):
                line = "max-txs = -1\n"
        elif section == "oracle":
            if stripped.startswith("enabled ="):
                line = "enabled = true\n"
            elif stripped.startswith("sidecar_socket ="):
                line = f'sidecar_socket = "{oracle_socket}"\n'
            elif stripped.startswith("sidecar_timeout ="):
                line = 'sidecar_timeout = "3s"\n'
        out.append(line)
with open(path, "w", encoding="utf-8") as handle:
    handle.writelines(out)
PY
}

assert_node_app_config() {
	local app="$1"
	local socket="$2"
	python3 - "$app" "$socket" <<'PY'
import sys

path, expected_socket = sys.argv[1:]
section = ""
values = {}
with open(path, "r", encoding="utf-8") as handle:
    for line in handle:
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            section = stripped.strip("[]")
            continue
        if "=" not in stripped or stripped.startswith("#"):
            continue
        key, raw = stripped.split("=", 1)
        values[(section, key.strip())] = raw.strip().strip('"')
errors = []
if values.get(("mempool", "max-txs")) != "-1":
    errors.append("mempool.max-txs must be -1")
if values.get(("oracle", "enabled")) != "true":
    errors.append("oracle.enabled must be true")
if values.get(("oracle", "sidecar_socket")) != expected_socket:
    errors.append("oracle.sidecar_socket mismatch")
if errors:
    raise SystemExit("; ".join(errors))
PY
}

edit_config_toml() {
	local cfg="$1"
	local rpc="$2"
	local p2p="$3"
	local proxy="$4"
	local pprof="$5"
	local peers="$6"
	python3 - "$cfg" "$rpc" "$p2p" "$proxy" "$pprof" "$peers" <<'PY'
import sys

path, rpc, p2p, proxy, pprof, peers = sys.argv[1:]
section = ""
out = []
with open(path, "r", encoding="utf-8") as handle:
    for line in handle:
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            section = stripped.strip("[]")
        if stripped.startswith("proxy_app ="):
            line = f'proxy_app = "tcp://127.0.0.1:{proxy}"\n'
        elif stripped.startswith("pprof_laddr ="):
            line = f'pprof_laddr = "localhost:{pprof}"\n'
        elif section == "rpc" and stripped.startswith("laddr ="):
            line = f'laddr = "tcp://127.0.0.1:{rpc}"\n'
        elif section == "p2p":
            if stripped.startswith("laddr ="):
                line = f'laddr = "tcp://127.0.0.1:{p2p}"\n'
            elif stripped.startswith("persistent_peers ="):
                line = f'persistent_peers = "{peers}"\n'
            elif stripped.startswith("addr_book_strict ="):
                line = "addr_book_strict = false\n"
            elif stripped.startswith("allow_duplicate_ip ="):
                line = "allow_duplicate_ip = true\n"
        elif stripped.startswith("timeout_commit ="):
            line = 'timeout_commit = "2s"\n'
        out.append(line)
with open(path, "w", encoding="utf-8") as handle:
    handle.writelines(out)
PY
}

patch_genesis() {
	local genesis_path="$1"
	jq --arg chain_id "$CHAIN_ID" --arg denom "$BOND_DENOM" '
		.chain_id = $chain_id |
		.app_state.staking.params.bond_denom = $denom |
		.app_state.mint.params.mint_denom = $denom |
		.app_state.evm.params.evm_denom = $denom |
		.consensus.params.abci.vote_extensions_enable_height = "1" |
		.app_state.oracle.params.min_validators = 4 |
		.app_state.oracle.params.min_sources = 3 |
		.app_state.oracle.params.history_limit = 100 |
		.app_state.oracle.tasks = [
			{"symbol":"BTC/USD","value_type":"VALUE_TYPE_NUMERIC","enabled":true,"submission_interval":1},
			{"symbol":"ETH/USD","value_type":"VALUE_TYPE_NUMERIC","enabled":true,"submission_interval":1},
			{"symbol":"SOL/USD","value_type":"VALUE_TYPE_NUMERIC","enabled":true,"submission_interval":1}
		] |
		.app_state.oracle.task_schedule = [
			{"symbol":"BTC/USD","height":"3"},
			{"symbol":"ETH/USD","height":"3"},
			{"symbol":"SOL/USD","height":"3"},
			{"symbol":"BTC/USD","height":"4"},
			{"symbol":"ETH/USD","height":"4"},
			{"symbol":"SOL/USD","height":"4"}
		] |
		.app_state.oracle.latest_values = [] |
		.app_state.oracle.history = []
	' "$genesis_path" >"$BASE/genesis.patched.json"
	mv "$BASE/genesis.patched.json" "$genesis_path"
}

validate_genesis_contract() {
	local genesis_path="$1"
	jq -e '
		.app_state.oracle.params.min_validators == 4 and
		.app_state.oracle.params.min_sources == 3 and
		([.app_state.oracle.tasks[] | select(.enabled == true) | .symbol] | sort) == ["BTC/USD","ETH/USD","SOL/USD"] and
		([.app_state.oracle.task_schedule[] | .symbol] | length) == 6 and
		([.app_state.oracle.task_schedule[] | select(.height == "3" or .height == 3)] | length) == 3 and
		([.app_state.oracle.task_schedule[] | select(.height == "4" or .height == 4)] | length) == 3 and
		((.consensus.params.abci.vote_extensions_enable_height // "0") | tonumber) > 0 and
		((.app_state.feemarket.params.min_gas_price // "0") | tonumber) > 0
	' "$genesis_path" >/dev/null
}

for i in 1 2 3 4; do
	"$BIN" init "val$i" \
		--chain-id "$CHAIN_ID" \
		--home "${HOMES[$i]}" \
		--default-denom "$BOND_DENOM" \
		--constitution-base-address "$BASE_ADDR" \
		--constitution-moderator-address "$MOD_ADDR" \
		>"$BASE/logs/init-val$i.log" 2>&1
	add_key_address "val$i" "${HOMES[$i]}" >/dev/null
	TX_ADDRS[$i]="$(add_key_address "agent$i" "${HOMES[$i]}")"
done

GENESIS_HOME="${HOMES[1]}"
GENESIS_PATH="$GENESIS_HOME/config/genesis.json"
patch_genesis "$GENESIS_PATH"

for i in 1 2 3 4; do
	addr="$("$BIN" keys show "val$i" -a --keyring-backend test --home "${HOMES[$i]}")"
	"$BIN" genesis add-genesis-account "$addr" "$BALANCE" --home "$GENESIS_HOME" >"$BASE/logs/add-genesis-val-$i.log" 2>&1
	"$BIN" genesis add-genesis-account "${TX_ADDRS[$i]}" "$TX_BALANCE" --home "$GENESIS_HOME" >"$BASE/logs/add-genesis-agent-$i.log" 2>&1
done

for i in 2 3 4; do
	cp "$GENESIS_PATH" "${HOMES[$i]}/config/genesis.json"
done

for i in 1 2 3 4; do
	"$BIN" genesis gentx "val$i" "$STAKE" \
		--chain-id "$CHAIN_ID" \
		--keyring-backend test \
		--home "${HOMES[$i]}" \
		--gas 200000 \
		--gas-prices "$GENTX_GAS_PRICE" \
		>"$BASE/logs/gentx-$i.log" 2>&1
done

mkdir -p "$GENESIS_HOME/config/gentx"
for i in 2 3 4; do
	cp "${HOMES[$i]}"/config/gentx/*.json "$GENESIS_HOME/config/gentx/"
done
"$BIN" genesis collect-gentxs --home "$GENESIS_HOME" >"$BASE/logs/collect-gentxs.log" 2>&1
validate_genesis_contract "$GENESIS_PATH"
"$BIN" genesis validate "$GENESIS_PATH" --home "$GENESIS_HOME" >"$BASE/logs/validate-genesis.log" 2>&1

for i in 2 3 4; do
	cp "$GENESIS_PATH" "${HOMES[$i]}/config/genesis.json"
done

for i in 1 2 3 4; do
	NODE_IDS[$i]="$("$BIN" comet show-node-id --home "${HOMES[$i]}")"
	PEERS[$i]="${NODE_IDS[$i]}@127.0.0.1:${P2P_PORTS[$i]}"
done

for i in 1 2 3 4; do
	peers=""
	for j in 1 2 3 4; do
		if [[ "$j" != "$i" ]]; then
			if [[ -n "$peers" ]]; then
				peers="$peers,"
			fi
			peers="$peers${PEERS[$j]}"
		fi
	done
	edit_config_toml "${HOMES[$i]}/config/config.toml" "${RPC_PORTS[$i]}" "${P2P_PORTS[$i]}" "${PROXY_PORTS[$i]}" "${PPROF_PORTS[$i]}" "$peers"
	edit_app_toml "${HOMES[$i]}/config/app.toml" "tcp://127.0.0.1:${API_PORTS[$i]}" "127.0.0.1:${GRPC_PORTS[$i]}" "127.0.0.1:${JSONRPC_PORTS[$i]}" "127.0.0.1:${JSONRPC_WS_PORTS[$i]}" "${ORACLE_SOCKETS[$i]}"
	assert_node_app_config "${HOMES[$i]}/config/app.toml" "${ORACLE_SOCKETS[$i]}"
done

for i in 1 2 3 4; do
	if [[ "${#ORACLE_SOCKETS[$i]}" -gt 100 ]]; then
		echo "oracle socket path is too long for portable Unix sockets: ${ORACLE_SOCKETS[$i]}" >&2
		echo "set BASE to a short empty path such as /tmp/guru-oracle-4v" >&2
		exit 1
	fi
	"$ORACLED" init --home "${ORACLE_HOMES[$i]}" >"$BASE/logs/oracled-init-$i.log" 2>&1
	require_file_mode "${ORACLE_HOMES[$i]}" "700"
	require_file_mode "${ORACLE_HOMES[$i]}/config.toml" "600"
	require_file_mode "${ORACLE_HOMES[$i]}/sources.toml" "600"
	"$ORACLED" validate --home "${ORACLE_HOMES[$i]}" >"$BASE/logs/oracled-validate-$i.log" 2>&1
done

dump_diagnostics() {
	local label="$1"
	local i file
	echo "diagnostics=$label base_dir=$BASE" >&2
	for file in "$BASE"/logs/oracled-status-*.json "$BASE"/logs/oracled-status-*.err "$BASE"/logs/oracled-reconcile-*.json "$BASE"/logs/oracled-reconcile-*.err "$BASE"/logs/latest-values.json "$BASE"/logs/query-latest.err; do
		if [[ -f "$file" ]]; then
			echo "---- $file ----" >&2
			tail -n 120 "$file" >&2 || true
		fi
	done
	for i in 1 2 3 4; do
		if [[ -f "$BASE/logs/oracled-$i.log" ]]; then
			echo "---- oracled-$i.log ----" >&2
			tail -n 120 "$BASE/logs/oracled-$i.log" >&2 || true
		fi
		if [[ -f "$BASE/logs/gurud-$i.log" ]]; then
			echo "---- gurud-$i.log ----" >&2
			tail -n 120 "$BASE/logs/gurud-$i.log" >&2 || true
		fi
	done
}

check_owned_pids() {
	local pid idx log
	idx=0
	while [[ "$idx" -lt "${#ORACLE_PIDS[@]}" ]]; do
		pid="${ORACLE_PIDS[$idx]}"
		if ! pid_alive "$pid" || ! process_identity_matches "$pid" "${ORACLE_HOMES[$((idx + 1))]}"; then
			log="$BASE/logs/oracled-$((idx + 1)).log"
			echo "oracled $((idx + 1)) exited early; log follows" >&2
			tail -n 120 "$log" >&2 || true
			dump_diagnostics "oracled-$((idx + 1))-exited"
			exit 1
		fi
		idx=$((idx + 1))
	done
	idx=0
	while [[ "$idx" -lt "${#NODE_PIDS[@]}" ]]; do
		pid="${NODE_PIDS[$idx]}"
		if ! pid_alive "$pid" || ! process_identity_matches "$pid" "${HOMES[$((idx + 1))]}"; then
			log="$BASE/logs/gurud-$((idx + 1)).log"
			echo "gurud $((idx + 1)) exited early; log follows" >&2
			tail -n 120 "$log" >&2 || true
			dump_diagnostics "gurud-$((idx + 1))-exited"
			exit 1
		fi
		idx=$((idx + 1))
	done
}

status_ready() {
	local file="$1"
	jq -e '
		.command == "status" and
		.data.health == "healthy" and
		(.data.feeds | length) == 3 and
		([.data.feeds[].symbol] | sort) == ["BTC/USD","ETH/USD","SOL/USD"] and
		([.data.feeds[] | select(
			.freshness == "fresh" and
			.health == "healthy" and
			.latest != null and
			(.latest.successful_source_count | tonumber) >= 3
		)] | length) == 3
	' "$file" >/dev/null
}

wait_for_sidecar_ready() {
	local i="$1"
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	local status_file="$BASE/logs/oracled-status-$i.json"
	while true; do
		check_owned_pids
		if "$ORACLED" status --home "${ORACLE_HOMES[$i]}" --format json >"$status_file" 2>"$BASE/logs/oracled-status-$i.err"; then
			if status_ready "$status_file"; then
				echo "sidecar_ready=$i"
				return
			fi
		fi
		if (( SECONDS > deadline )); then
			echo "oracled $i did not become healthy/fresh" >&2
			cat "$status_file" >&2 || true
			cat "$BASE/logs/oracled-status-$i.err" >&2 || true
			tail -n 120 "$BASE/logs/oracled-$i.log" >&2 || true
			dump_diagnostics "sidecar-$i-timeout"
			exit 1
		fi
		sleep 2
	done
}

for i in 1 2 3 4; do
	"$ORACLED" start --home "${ORACLE_HOMES[$i]}" >"$BASE/logs/oracled-$i.log" 2>&1 &
	ORACLE_PIDS+=("$!")
	OWNED_PIDS+=("$!")
	OWNED_PID_IDENTITIES+=("${ORACLE_HOMES[$i]}")
done

for i in 1 2 3 4; do
	wait_for_sidecar_ready "$i"
done

for i in 1 2 3 4; do
	"$BIN" start \
		--home "${HOMES[$i]}" \
		--chain-id "$CHAIN_ID" \
		--evm.evm-chain-id "$EVM_CHAIN_ID" \
		--minimum-gas-prices "$NODE_MINIMUM_GAS_PRICES" \
		--evm.min-tip 0 \
		--log_level info \
		>"$BASE/logs/gurud-$i.log" 2>&1 &
	NODE_PIDS+=("$!")
	OWNED_PIDS+=("$!")
	OWNED_PID_IDENTITIES+=("${HOMES[$i]}")
done

wait_for_rpc() {
	local i="$1"
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	while true; do
		check_owned_pids
		if "$BIN" status --node "tcp://127.0.0.1:${RPC_PORTS[$i]}" >/dev/null 2>&1; then
			echo "rpc_ready=$i"
			return
		fi
		if (( SECONDS > deadline )); then
			echo "node $i RPC did not start" >&2
			tail -n 120 "$BASE/logs/gurud-$i.log" >&2 || true
			dump_diagnostics "rpc-$i-timeout"
			exit 1
		fi
		sleep 1
	done
}

for i in 1 2 3 4; do
	wait_for_rpc "$i"
done

node_height() {
	local i="$1"
	"$BIN" status --node "tcp://127.0.0.1:${RPC_PORTS[$i]}" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"'
}

wait_for_all_heights() {
	local target="$1"
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	local i height all_ready heights
	while true; do
		check_owned_pids
		all_ready=1
		heights=""
		for i in 1 2 3 4; do
			height="$(node_height "$i")"
			heights="${heights} node${i}=${height}"
			if [[ ! "$height" =~ ^[0-9]+$ ]] || (( height < target )); then
				all_ready=0
			fi
		done
		if [[ "$all_ready" -eq 1 ]]; then
			echo "heights_target=$target$heights"
			return
		fi
		if (( SECONDS > deadline )); then
			echo "all nodes did not reach height $target; last heights:$heights" >&2
			for i in 1 2 3 4; do
				tail -n 80 "$BASE/logs/gurud-$i.log" >&2 || true
			done
			dump_diagnostics "height-$target-timeout"
			exit 1
		fi
		sleep 1
	done
}

wait_for_all_increasing() {
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	local i height all_increased heights
	while true; do
		check_owned_pids
		all_increased=1
		heights=""
		for i in 1 2 3 4; do
			height="$(node_height "$i")"
			heights="${heights} node${i}=${INITIAL_HEIGHTS[$i]}->$height"
			if [[ ! "$height" =~ ^[0-9]+$ ]] || (( height <= INITIAL_HEIGHTS[$i] )); then
				all_increased=0
			fi
		done
		if [[ "$all_increased" -eq 1 ]]; then
			echo "heights_increased$heights"
			return
		fi
		if (( SECONDS > deadline )); then
			echo "all nodes did not produce an additional block; last heights:$heights" >&2
			dump_diagnostics "height-increase-timeout"
			exit 1
		fi
		sleep 1
	done
}

declare -a INITIAL_HEIGHTS
wait_for_all_heights 4
for i in 1 2 3 4; do
	INITIAL_HEIGHTS[$i]="$(node_height "$i")"
done
wait_for_all_increasing

validators_file="$BASE/logs/validators.json"
if ! curl -fsS "http://127.0.0.1:${RPC_PORTS[1]}/validators" >"$validators_file"; then
	echo "failed to query validator set" >&2
	dump_diagnostics "validator-query-failed"
	exit 1
fi
if ! validator_count="$(jq -er '.result.validators | length' "$validators_file")" || [[ "$validator_count" != "4" ]]; then
	echo "expected validator set size 4, got $validator_count" >&2
	dump_diagnostics "validator-count-mismatch"
	exit 1
fi
echo "validator_set_size=4"

reconcile_ready() {
	local file="$1"
	jq -e '
		.command == "reconcile" and
		.data.active_task_count == 3 and
		.data.min_sources == 3 and
		([.data.findings[]? | select(.blocking == true)] | length) == 0
	' "$file" >/dev/null
}

for i in 1 2 3 4; do
	reconcile_file="$BASE/logs/oracled-reconcile-$i.json"
	if ! "$ORACLED" reconcile --home "${ORACLE_HOMES[$i]}" --node-grpc "127.0.0.1:${GRPC_PORTS[$i]}" --format json >"$reconcile_file" 2>"$BASE/logs/oracled-reconcile-$i.err"; then
		echo "oracled $i reconcile command failed" >&2
		cat "$reconcile_file" >&2 || true
		cat "$BASE/logs/oracled-reconcile-$i.err" >&2 || true
		dump_diagnostics "reconcile-$i-command-failed"
		exit 1
	fi
	if ! reconcile_ready "$reconcile_file"; then
		echo "oracled $i reconcile did not pass" >&2
		cat "$reconcile_file" >&2
		dump_diagnostics "reconcile-$i-predicate-failed"
		exit 1
	fi
	echo "reconcile_ready=$i"
done

latest_file="$BASE/logs/latest-values.json"
deadline=$((SECONDS + WAIT_TIMEOUT))
while true; do
	check_owned_pids
	if "$BIN" query oracle latest-values \
		--node "tcp://127.0.0.1:${RPC_PORTS[1]}" \
		--home "${HOMES[1]}" \
		--chain-id "$CHAIN_ID" \
		--output json >"$latest_file" 2>"$BASE/logs/query-latest.err"; then
		if jq -e '
			def positive_number($value): (($value | tostring | tonumber) > 0);
			. as $root |
			(["BTC/USD", "ETH/USD", "SOL/USD"] as $symbols |
				([$root.values[]?.symbol] | sort) == $symbols and
				all($symbols[]; . as $symbol |
					([$root.values[]? | select(.symbol == $symbol)] | length) == 1 and
					($root.values[]? | select(.symbol == $symbol) |
						positive_number(.value) and
						positive_number(.block_height) and
						positive_number(.block_time_unix)
					)
				)
			)
		' "$latest_file" >/dev/null; then
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
		echo "oracle latest values did not appear for every required symbol" >&2
		cat "$latest_file" >&2 || true
		cat "$BASE/logs/query-latest.err" >&2 || true
		for i in 1 2 3 4; do
			tail -n 120 "$BASE/logs/oracled-$i.log" >&2 || true
			grep -i oracle "$BASE/logs/gurud-$i.log" | tail -n 120 >&2 || true
		done
		dump_diagnostics "latest-values-timeout"
		exit 1
	fi
	sleep 2
done

ready_env="$BASE/ready.env"
{
	printf 'BASE=%q\n' "$BASE"
	printf 'REPO=%q\n' "$REPO"
	printf 'BIN=%q\n' "$BIN"
	printf 'ORACLED=%q\n' "$ORACLED"
	printf 'CHAIN_ID=%q\n' "$CHAIN_ID"
	printf 'EVM_CHAIN_ID=%q\n' "$EVM_CHAIN_ID"
	for i in 1 2 3 4; do
		printf 'HOME_%s=%q\n' "$i" "${HOMES[$i]}"
		printf 'RPC_%s=%q\n' "$i" "${RPC_PORTS[$i]}"
		printf 'GRPC_%s=%q\n' "$i" "${GRPC_PORTS[$i]}"
		printf 'ORACLE_HOME_%s=%q\n' "$i" "${ORACLE_HOMES[$i]}"
		printf 'ORACLE_SOCKET_%s=%q\n' "$i" "${ORACLE_SOCKETS[$i]}"
		printf 'AGENT_NAME_%s=%q\n' "$i" "agent$i"
		printf 'AGENT_ADDR_%s=%q\n' "$i" "${TX_ADDRS[$i]}"
	done
} >"$ready_env"
require_file_mode "$ready_env" "600"

echo "ready_env=$ready_env"
echo "stop_file=$BASE/stop"
echo "testnet_ready=1"

if [[ "$EXIT_AFTER_READY" -eq 1 ]]; then
	exit 0
fi

while [[ ! -f "$BASE/stop" ]]; do
	check_owned_pids
	sleep 1
done
