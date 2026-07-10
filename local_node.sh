#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${BIN:-$SCRIPT_DIR/build/gurud}"
ORACLED="${ORACLED:-$SCRIPT_DIR/build/oracled}"

CHAIN_ID="${CHAIN_ID:-guru_631}"
CHAINDIR="${CHAINDIR:-$HOME/.gurud-local}"
MONIKER="${MONIKER:-local-validator}"
KEYRING="${KEYRING:-test}"
KEYALGO="${KEYALGO:-eth_secp256k1}"
BOND_DENOM="${BOND_DENOM:-agxn}"
DISPLAY_DENOM="${DISPLAY_DENOM:-gxn}"
BALANCE="${BALANCE:-100000000000000000000000000${BOND_DENOM}}"
STAKE="${STAKE:-1000000000000000000000000${BOND_DENOM}}"
MIN_GAS_PRICE="${MIN_GAS_PRICE:-0${BOND_DENOM}}"
BASE_FEE="${BASE_FEE:-0}"
GENESIS_MIN_GAS_PRICE="${GENESIS_MIN_GAS_PRICE:-0}"

RPC_LADDR="${RPC_LADDR:-tcp://127.0.0.1:26657}"
P2P_LADDR="${P2P_LADDR:-tcp://127.0.0.1:26656}"
PROXY_APP="${PROXY_APP:-tcp://127.0.0.1:26658}"
API_ADDRESS="${API_ADDRESS:-tcp://127.0.0.1:1317}"
GRPC_ADDRESS="${GRPC_ADDRESS:-127.0.0.1:9090}"
GRPC_WEB_ADDRESS="${GRPC_WEB_ADDRESS:-127.0.0.1:9091}"
JSONRPC_ADDRESS="${JSONRPC_ADDRESS:-127.0.0.1:8545}"
JSONRPC_WS_ADDRESS="${JSONRPC_WS_ADDRESS:-127.0.0.1:8546}"
ORACLE_SOCKET="${ORACLE_SOCKET:-}"
PRICE_URL="${PRICE_URL:-https://api.coinbase.com/v2/prices/BTC-USD/spot}"
ORACLE_SYMBOL="${ORACLE_SYMBOL:-BTC/USD}"

BUILD=true
OVERWRITE=""
START_NODE=true
WITH_ORACLE=false

VAL_KEY="validator"
DEV_KEYS=(dev0 dev1 dev2 dev3)
CONSTITUTION_BASE_KEY="constitution-base"
CONSTITUTION_MODERATOR_KEY="constitution-moderator"

usage() {
	cat <<EOF
Usage: $0 [options]

Options:
  -y, --yes            Overwrite existing local node home without prompting
  -n, --no             Reuse existing local node home and just start the node
  --home PATH          Node home directory (default: $CHAINDIR)
  --chain-id ID        Chain ID (default: $CHAIN_ID)
  --no-build           Do not run 'make build' even if build/gurud is missing
  --no-start           Initialize/configure only; do not start the node
  --with-oracle        Enable one-validator oracle task and start oracled
  -h, --help           Show this help

Environment overrides:
  BIN, ORACLED, BOND_DENOM, BALANCE, STAKE, MIN_GAS_PRICE,
  BASE_FEE, GENESIS_MIN_GAS_PRICE,
  RPC_LADDR, P2P_LADDR, API_ADDRESS, GRPC_ADDRESS,
  JSONRPC_ADDRESS, JSONRPC_WS_ADDRESS, ORACLE_SYMBOL, PRICE_URL
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		-y | --yes)
			OVERWRITE="y"
			shift
			;;
		-n | --no)
			OVERWRITE="n"
			shift
			;;
		--home)
			CHAINDIR="$2"
			shift 2
			;;
		--chain-id)
			CHAIN_ID="$2"
			shift 2
			;;
		--no-build)
			BUILD=false
			shift
			;;
		--no-start)
			START_NODE=false
			shift
			;;
		--with-oracle)
			WITH_ORACLE=true
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "unknown option: $1" >&2
			usage
			exit 1
			;;
	esac
done

CONFIG_TOML="$CHAINDIR/config/config.toml"
APP_TOML="$CHAINDIR/config/app.toml"
GENESIS="$CHAINDIR/config/genesis.json"
TMP_GENESIS="$CHAINDIR/config/tmp_genesis.json"
ORACLE_HOME="$CHAINDIR/oracled"

if [[ -z "$ORACLE_SOCKET" ]]; then
	ORACLE_SOCKET="$CHAINDIR/oracle.sock"
fi

require_tool() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required tool: $1" >&2
		exit 1
	fi
}

require_tool jq
require_tool perl
require_tool python3

if [[ "$BUILD" == true && ! -x "$BIN" ]]; then
	echo "building local binaries..."
	(cd "$SCRIPT_DIR" && make build)
fi

if [[ ! -x "$BIN" ]]; then
	echo "missing gurud binary: $BIN" >&2
	echo "run 'make build' or pass BIN=/path/to/gurud" >&2
	exit 1
fi

if [[ "$WITH_ORACLE" == true && ! -x "$ORACLED" ]]; then
	echo "missing oracled binary: $ORACLED" >&2
	echo "run 'make build' or pass ORACLED=/path/to/oracled" >&2
	exit 1
fi

if [[ -z "$OVERWRITE" ]]; then
	if [[ -d "$CHAINDIR" ]]; then
		printf "Existing node home found at %s. Overwrite? [y/n] " "$CHAINDIR"
		read -r OVERWRITE
	else
		OVERWRITE="y"
	fi
fi

add_key_if_missing() {
	local name="$1"
	if "$BIN" keys show "$name" --keyring-backend "$KEYRING" --home "$CHAINDIR" >/dev/null 2>&1; then
		return
	fi
	"$BIN" keys add "$name" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR" --output json \
		>"$CHAINDIR/key-$name.json"
}

key_addr() {
	"$BIN" keys show "$1" -a --keyring-backend "$KEYRING" --home "$CHAINDIR"
}

set_toml() {
	local path="$1"
	local section="$2"
	local key="$3"
	local value="$4"
	python3 - "$path" "$section" "$key" "$value" <<'PY'
import sys

path, section, key, value = sys.argv[1:]
current = ""
out = []
changed = False
seen_section = section == ""
inserted = False

def section_name(line):
    stripped = line.strip()
    if stripped.startswith("[") and stripped.endswith("]"):
        return stripped.strip("[]")
    return None

def is_key_line(line):
    stripped = line.strip()
    return stripped.startswith(key + " =") or stripped.startswith(key + "=")

def value_line():
    return f"{key} = {value}\n"

with open(path, "r", encoding="utf-8") as f:
    for line in f:
        next_section = section_name(line)
        if next_section is not None:
            if current == section and seen_section and not changed and not inserted:
                out.append(value_line())
                inserted = True
                changed = True
            current = next_section
            if current == section:
                seen_section = True
            out.append(line)
            continue

        if current == section and is_key_line(line):
            out.append(value_line())
            changed = True
            inserted = True
            continue

        out.append(line)

if not changed:
    if seen_section:
        out.append(value_line())
    elif section:
        out.append(f"\n[{section}]\n{value_line()}")
    else:
        out.insert(0, value_line())

with open(path, "w", encoding="utf-8") as f:
    f.writelines(out)
PY
}

configure_app_toml() {
	set_toml "$APP_TOML" "api" "enable" "true"
	set_toml "$APP_TOML" "api" "address" "\"$API_ADDRESS\""
	set_toml "$APP_TOML" "grpc" "enable" "true"
	set_toml "$APP_TOML" "grpc" "address" "\"$GRPC_ADDRESS\""
	set_toml "$APP_TOML" "grpc-web" "enable" "true"
	set_toml "$APP_TOML" "grpc-web" "address" "\"$GRPC_WEB_ADDRESS\""
	set_toml "$APP_TOML" "json-rpc" "enable" "true"
	set_toml "$APP_TOML" "json-rpc" "address" "\"$JSONRPC_ADDRESS\""
	set_toml "$APP_TOML" "json-rpc" "ws-address" "\"$JSONRPC_WS_ADDRESS\""
	set_toml "$APP_TOML" "json-rpc" "api" "\"eth,txpool,personal,net,debug,web3\""
	set_toml "$APP_TOML" "oracle" "enabled" "$WITH_ORACLE"
	set_toml "$APP_TOML" "oracle" "sidecar_socket" "\"$ORACLE_SOCKET\""
	set_toml "$APP_TOML" "oracle" "sidecar_timeout" "\"3s\""
	set_toml "$APP_TOML" "" "pruning" "\"custom\""
	set_toml "$APP_TOML" "" "pruning-keep-recent" "\"100\""
	set_toml "$APP_TOML" "" "pruning-interval" "\"10\""
}

configure_config_toml() {
	perl -0pi -e "s#proxy_app = \"tcp://127\\.0\\.0\\.1:26658\"#proxy_app = \"$PROXY_APP\"#g" "$CONFIG_TOML"
	perl -0pi -e "s#laddr = \"tcp://127\\.0\\.0\\.1:26657\"#laddr = \"$RPC_LADDR\"#g" "$CONFIG_TOML"
	perl -0pi -e "s#laddr = \"tcp://0\\.0\\.0\\.0:26656\"#laddr = \"$P2P_LADDR\"#g" "$CONFIG_TOML"
	perl -0pi -e 's#type = "flood"#type = "app"#g' "$CONFIG_TOML"
	perl -0pi -e 's#timeout_commit = "5s"#timeout_commit = "1s"#g' "$CONFIG_TOML"
	perl -0pi -e 's#timeout_commit = "500ms"#timeout_commit = "1s"#g' "$CONFIG_TOML"
	perl -0pi -e 's#addr_book_strict = true#addr_book_strict = false#g' "$CONFIG_TOML"
	perl -0pi -e 's#allow_duplicate_ip = false#allow_duplicate_ip = true#g' "$CONFIG_TOML"
}

customize_genesis() {
	jq \
		--arg bond_denom "$BOND_DENOM" \
		--arg display_denom "$DISPLAY_DENOM" \
		--arg base_fee "$BASE_FEE" \
		--arg min_gas_price "$GENESIS_MIN_GAS_PRICE" \
		--argjson with_oracle "$WITH_ORACLE" \
		--arg oracle_symbol "$ORACLE_SYMBOL" '
		(.app_state.staking.params.bond_denom? // empty) as $ignore |
		.app_state.staking.params.bond_denom = $bond_denom |
		(if .app_state.gov.params.min_deposit then
			.app_state.gov.params.min_deposit[0].denom = $bond_denom
		else . end) |
		(if .app_state.gov.params.expedited_min_deposit then
			.app_state.gov.params.expedited_min_deposit[0].denom = $bond_denom
		else . end) |
		(if .app_state.evm.params.evm_denom then
			.app_state.evm.params.evm_denom = $bond_denom
		else . end) |
		(if .app_state.bank.denom_metadata then
			.app_state.bank.denom_metadata = [{
				"description": "The native token for local Guru development.",
				"denom_units": [
					{"denom": $bond_denom, "exponent": 0, "aliases": ["atto" + $display_denom]},
					{"denom": $display_denom, "exponent": 18, "aliases": []}
				],
				"base": $bond_denom,
				"display": $display_denom,
				"name": "Guru",
				"symbol": "GXN",
				"uri": "",
				"uri_hash": ""
			}]
		else . end) |
		(if .app_state.feemarket.params then
			.app_state.feemarket.params.base_fee = $base_fee |
			.app_state.feemarket.params.min_gas_price = $min_gas_price |
			.app_state.feemarket.params.no_base_fee = true
		else . end) |
		(if .app_state.oracle.params then
			.app_state.oracle.params.min_validators = 1 |
			.app_state.oracle.params.min_sources = 1 |
			.app_state.oracle.params.history_limit = 100 |
			if $with_oracle then
				.app_state.oracle.tasks = [{
					"symbol": $oracle_symbol,
					"value_type": 1,
					"enabled": true,
					"submission_interval": 1
				}] |
				.app_state.oracle.task_schedule = [
					{"symbol": $oracle_symbol, "height": 1},
					{"symbol": $oracle_symbol, "height": 2}
				]
			else . end |
			.app_state.oracle.latest_values = [] |
			.app_state.oracle.history = []
		else . end) |
		(if .app_state.constitution.params.oracle_fee_market then
			.app_state.constitution.params.oracle_fee_market = {
				"enabled": $with_oracle,
				"symbol": $oracle_symbol,
				"multiplier": "0.00000063",
				"max_change_rate": "0.10"
			}
		else . end) |
		.consensus.params.block.max_gas = "10000000"
	' "$GENESIS" >"$TMP_GENESIS"
	mv "$TMP_GENESIS" "$GENESIS"
}

write_oracled_config() {
	mkdir -p "$ORACLE_HOME/config"
	cat >"$ORACLE_HOME/config/oracled.toml" <<EOF
socket = "$ORACLE_SOCKET"
request_timeout = "3s"
source_timeout = "2s"
node_grpc = "$GRPC_ADDRESS"
node_query_timeout = "3s"

[[sources]]
name = "local-${ORACLE_SYMBOL//\//-}"
symbol = "$ORACLE_SYMBOL"
value_type = "NUMERIC"
url = "$PRICE_URL"
response_path = "data.amount"
timeout = "2s"
interval = "1s"
EOF
}

if [[ "$OVERWRITE" == "y" || "$OVERWRITE" == "Y" ]]; then
	rm -rf "$CHAINDIR"
	mkdir -p "$CHAINDIR"
	mkdir -p "$CHAINDIR/logs"

	"$BIN" config set client chain-id "$CHAIN_ID" --home "$CHAINDIR"
	"$BIN" config set client keyring-backend "$KEYRING" --home "$CHAINDIR"

	add_key_if_missing "$CONSTITUTION_BASE_KEY"
	add_key_if_missing "$CONSTITUTION_MODERATOR_KEY"
	BASE_ADDR="$(key_addr "$CONSTITUTION_BASE_KEY")"
	MOD_ADDR="$(key_addr "$CONSTITUTION_MODERATOR_KEY")"

	"$BIN" init "$MONIKER" \
		--chain-id "$CHAIN_ID" \
		--home "$CHAINDIR" \
		--constitution-base-address "$BASE_ADDR" \
		--constitution-moderator-address "$MOD_ADDR" \
		--overwrite \
		>"$CHAINDIR/logs/init.json" 2>&1

	add_key_if_missing "$VAL_KEY"
	for key in "${DEV_KEYS[@]}"; do
		add_key_if_missing "$key"
	done

	"$BIN" genesis add-genesis-account "$VAL_KEY" "$BALANCE" --keyring-backend "$KEYRING" --home "$CHAINDIR"
	for key in "${DEV_KEYS[@]}"; do
		"$BIN" genesis add-genesis-account "$key" "$BALANCE" --keyring-backend "$KEYRING" --home "$CHAINDIR"
	done
	"$BIN" genesis add-genesis-account "$CONSTITUTION_BASE_KEY" "$BALANCE" --keyring-backend "$KEYRING" --home "$CHAINDIR"
	"$BIN" genesis add-genesis-account "$CONSTITUTION_MODERATOR_KEY" "$BALANCE" --keyring-backend "$KEYRING" --home "$CHAINDIR"

	customize_genesis

	"$BIN" genesis gentx "$VAL_KEY" "$STAKE" \
		--chain-id "$CHAIN_ID" \
		--keyring-backend "$KEYRING" \
		--home "$CHAINDIR" \
		>"$CHAINDIR/logs/gentx.json" 2>&1
	"$BIN" genesis collect-gentxs --home "$CHAINDIR" >"$CHAINDIR/logs/collect-gentxs.json" 2>&1
	"$BIN" genesis validate "$GENESIS" --home "$CHAINDIR"

	configure_config_toml
	configure_app_toml
	if [[ "$WITH_ORACLE" == true ]]; then
		write_oracled_config
	fi
elif [[ "$OVERWRITE" != "n" && "$OVERWRITE" != "N" ]]; then
	echo "expected y or n, got: $OVERWRITE" >&2
	exit 1
fi

echo "home=$CHAINDIR"
echo "chain_id=$CHAIN_ID"
echo "rpc=$RPC_LADDR"
echo "api=$API_ADDRESS"
echo "grpc=$GRPC_ADDRESS"
echo "json_rpc=http://$JSONRPC_ADDRESS"
echo "json_rpc_ws=ws://$JSONRPC_WS_ADDRESS"
echo "validator=$VAL_KEY $(key_addr "$VAL_KEY" 2>/dev/null || true)"
for key in "${DEV_KEYS[@]}"; do
	echo "$key=$(key_addr "$key" 2>/dev/null || true)"
done

if [[ "$START_NODE" != true ]]; then
	echo "initialized_only=1"
	exit 0
fi

ORACLE_PID=""
NODE_PID=""
cleanup() {
	if [[ -n "$ORACLE_PID" ]]; then
		kill "$ORACLE_PID" >/dev/null 2>&1 || true
	fi
	if [[ -n "$NODE_PID" ]]; then
		kill "$NODE_PID" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

start_node_cmd=(
	"$BIN" start
	--home "$CHAINDIR"
	--chain-id "$CHAIN_ID"
	--minimum-gas-prices "$MIN_GAS_PRICE"
	--log_level info
	--json-rpc.api eth,txpool,personal,net,debug,web3
)

wait_for_rpc() {
	local rpc_node="$1"
	local deadline=$((SECONDS + 60))
	until "$BIN" status --node "$rpc_node" >/dev/null 2>&1; do
		if ((SECONDS > deadline)); then
			echo "node RPC did not start within 60s: $rpc_node" >&2
			return 1
		fi
		sleep 1
	done
}

if [[ "$WITH_ORACLE" == true ]]; then
	mkdir -p "$CHAINDIR/logs"
	"${start_node_cmd[@]}" >"$CHAINDIR/logs/gurud.log" 2>&1 &
	NODE_PID="$!"
	echo "gurud_pid=$NODE_PID"
	echo "gurud_log=$CHAINDIR/logs/gurud.log"
	wait_for_rpc "$RPC_LADDR"

	"$ORACLED" start --home "$ORACLE_HOME" >"$CHAINDIR/logs/oracled.log" 2>&1 &
	ORACLE_PID="$!"
	echo "oracled_pid=$ORACLE_PID"
	echo "oracled_log=$CHAINDIR/logs/oracled.log"
	wait "$NODE_PID"
else
	"${start_node_cmd[@]}"
fi
