#!/usr/bin/env bash
set -euo pipefail

if (( $# < 2 || $# > 3 )); then
	echo "usage: $0 READY_ENV WORKER_INDEX [COUNT]" >&2
	exit 2
fi

READY_ENV="$1"
WORKER_INDEX="$2"
COUNT="${3:-10}"

if [[ ! -f "$READY_ENV" ]]; then
	echo "ready env not found: $READY_ENV" >&2
	exit 1
fi
if ! [[ "$WORKER_INDEX" =~ ^[1-4]$ ]]; then
	echo "worker index must be 1..4: $WORKER_INDEX" >&2
	exit 1
fi
if ! [[ "$COUNT" =~ ^[0-9]+$ ]]; then
	echo "count must be a non-negative integer: $COUNT" >&2
	exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
	echo "missing required tool: jq" >&2
	exit 1
fi

# shellcheck disable=SC1090
source "$READY_ENV"

from_name_var="AGENT_NAME_$WORKER_INDEX"
from_home_var="HOME_$WORKER_INDEX"
from_name="${!from_name_var}"
from_home="${!from_home_var}"
recipient_index=$((WORKER_INDEX % 4 + 1))
to_addr_var="AGENT_ADDR_$recipient_index"
to_addr="${!to_addr_var}"

amount_base="${TX_AMOUNT_BASE:-1000}"
fee="${TX_FEE:-1000000agxn}"
gas="${TX_GAS:-250000}"
log_dir="$BASE/logs/tx-workers"
mkdir -p "$log_dir"
log_file="$log_dir/worker-$WORKER_INDEX.log"
summary_file="$log_dir/worker-$WORKER_INDEX.summary.json"

success=0
echo "worker=$WORKER_INDEX from=$from_name recipient_agent=$recipient_index count=$COUNT" >"$log_file"

for n in $(seq 1 "$COUNT"); do
	node_index=$(((WORKER_INDEX + n - 2) % 4 + 1))
	rpc_var="RPC_$node_index"
	node="tcp://127.0.0.1:${!rpc_var}"
	amount="$((amount_base + (RANDOM % 1000)))agxn"

	echo "send index=$n node=$node amount=$amount" >>"$log_file"
	if ! out="$("$BIN" tx bank send "$from_name" "$to_addr" "$amount" \
		--from "$from_name" \
		--keyring-backend test \
		--home "$from_home" \
		--chain-id "$CHAIN_ID" \
		--node "$node" \
		--gas "$gas" \
		--fees "$fee" \
		--broadcast-mode sync \
		--yes \
		--output json 2>&1)"; then
		echo "broadcast failed index=$n output=$out" >>"$log_file"
		exit 1
	fi
	echo "$out" >"$log_dir/worker-$WORKER_INDEX-tx-$n.broadcast.json"

	code="$(jq -r '.code // 0' <<<"$out")"
	if [[ "$code" != "0" ]]; then
		raw_log="$(jq -r '.raw_log // empty' <<<"$out")"
		echo "broadcast rejected index=$n code=$code raw_log=$raw_log" >>"$log_file"
		exit 1
	fi
	txhash="$(jq -r '.txhash // empty' <<<"$out")"
	if [[ -z "$txhash" || "$txhash" == "null" ]]; then
		echo "broadcast missing txhash index=$n output=$out" >>"$log_file"
		exit 1
	fi

	if ! wait_out="$("$BIN" query wait-tx "$txhash" \
		--node "$node" \
		--home "$from_home" \
		--timeout 60s \
		--output json 2>&1)"; then
		echo "wait-tx failed index=$n txhash=$txhash output=$wait_out" >>"$log_file"
		exit 1
	fi
	echo "$wait_out" >"$log_dir/worker-$WORKER_INDEX-tx-$n.wait.json"
	wait_code="$(jq -r '.code // 0' <<<"$wait_out")"
	if [[ "$wait_code" != "0" ]]; then
		raw_log="$(jq -r '.raw_log // empty' <<<"$wait_out")"
		echo "wait-tx rejected index=$n code=$wait_code raw_log=$raw_log txhash=$txhash" >>"$log_file"
		exit 1
	fi

	success=$((success + 1))
	sleep "0.$((2 + (RANDOM % 7)))"
done

jq -n \
	--argjson worker "$WORKER_INDEX" \
	--argjson success "$success" \
	--argjson count "$COUNT" \
	--arg from "$from_name" \
	--arg to "$to_addr" \
	'{worker:$worker, success:$success, count:$count, from:$from, to:$to}' \
	>"$summary_file"
cat "$summary_file"
