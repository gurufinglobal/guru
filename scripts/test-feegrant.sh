#!/usr/bin/env bash
set -euo pipefail

#
# Feegrant E2E test script (Cosmos Tx + EVM Tx)
# - Uses the local keys/config created by ./local_node.sh (dev0/dev1/dev2 ...)
# - Validates that fees are paid by the fee granter (dev0) for txs signed by the grantee (dev1)
#

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

CHAIN_ID="${CHAIN_ID:-guru_631-1}"
DENOM="${DENOM:-agxn}"
HOME_DIR="${HOME_DIR:-${HOME}/.gurud}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
NODE_RPC="${NODE_RPC:-tcp://localhost:26657}"
ETH_RPC="${ETH_RPC:-http://127.0.0.1:8545}"

# NOTE: local_node.sh sets BASEFEE=630000000000 (agxn per gas unit) and the chain enforces it as a minimum fee.
GAS_PRICES="${GAS_PRICES:-630000000000${DENOM}}"
GAS="${GAS:-auto}"
GAS_ADJUSTMENT="${GAS_ADJUSTMENT:-2.0}"
TX_RETRIES="${TX_RETRIES:-5}"

GRANTER_KEY="${GRANTER_KEY:-dev0}"
GRANTEE_KEY="${GRANTEE_KEY:-dev1}"
RECIPIENT_KEY="${RECIPIENT_KEY:-dev2}"

SPEND_LIMIT="${SPEND_LIMIT:-}"
COSMOS_SEND_AMOUNT="${COSMOS_SEND_AMOUNT:-1${DENOM}}"
EVM_SEND_AMOUNT="${EVM_SEND_AMOUNT:-1${DENOM}}"

START_NODE="${START_NODE:-auto}" # auto|yes|no
RESET_CHAIN="${RESET_CHAIN:-0}"  # 1 => wipe and re-init via local_node.sh -y
SKIP_INSTALL="${SKIP_INSTALL:-0}" # 1 => pass --no-install to local_node.sh

ONLY="${ONLY:-all}" # all|cosmos|evm

die() {
  echo "ERROR: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

gurud_cmd() {
  gurud \
    --home "$HOME_DIR" \
    --keyring-backend "$KEYRING_BACKEND" \
    --chain-id "$CHAIN_ID" \
    --node "$NODE_RPC" \
    "$@"
}

status_ok() {
  # CometBFT RPC is HTTP even if --node uses tcp://
  local url
  url="$(echo "$NODE_RPC" | sed 's#^tcp://#http://#')/status"
  curl -sf "$url" >/dev/null 2>&1
}

status_height() {
  local url
  url="$(echo "$NODE_RPC" | sed 's#^tcp://#http://#')/status"
  curl -sf "$url" | jq -r '.result.sync_info.latest_block_height // empty'
}

eth_rpc_ok() {
  curl -sf -H 'Content-Type: application/json' \
    --data '{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}' \
    "$ETH_RPC" >/dev/null 2>&1
}

wait_for_eth_rpc() {
  local timeout_s="${1:-60}"
  local i
  for ((i=0; i<timeout_s; i++)); do
    if eth_rpc_ok; then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_node() {
  local timeout_s="${1:-60}"
  local i
  local h
  for ((i=0; i<timeout_s; i++)); do
    if status_ok; then
      h="$(status_height || true)"
      if [[ -n "$h" && "$h" != "0" && "$h" != "null" ]]; then
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

wait_for_tx_success() {
  local txhash="$1"
  local timeout_s="${2:-60}"
  local i
  local out
  local code
  for ((i=0; i<timeout_s; i++)); do
    if out="$(gurud_cmd query tx "$txhash" -o json 2>/dev/null)"; then
      code="$(echo "$out" | jq -r '.code // 0')"
      if [[ "$code" == "0" ]]; then
        return 0
      fi
      echo "$out" | jq '{height,code,codespace,raw_log}'
      die "tx failed (code=$code) txhash=$txhash"
    fi
    sleep 1
  done
  return 1
}

addr_bech32() {
  local key="$1"
  gurud_cmd keys show "$key" -a
}

addr_hex() {
  local bech="$1"
  local hex
  hex="$(gurud debug addr "$bech" | awk -F': ' '/Address \(hex\):/{print $2; exit}')"
  [[ -n "$hex" ]] || return 1
  echo "0x$(echo "$hex" | tr '[:upper:]' '[:lower:]')"
}

balance_amount() {
  local bech="$1"
  gurud_cmd query bank balances "$bech" -o json \
    | jq -r --arg denom "$DENOM" '
        (.balances // [])
        | map(select(.denom == $denom))
        | (.[0].amount // "0")
      '
}

grant_exists() {
  local granter="$1"
  local grantee="$2"
  gurud_cmd query feegrant grant "$granter" "$grantee" -o json >/dev/null 2>&1
}

assert_fee_granter() {
  local txhash="$1"
  local expected_granter="$2"

  local granter_in_tx
  granter_in_tx="$(
    gurud_cmd query tx "$txhash" -o json \
      | jq -r '.tx.auth_info.fee.granter // ""'
  )"

  if [[ "$granter_in_tx" != "$expected_granter" ]]; then
    echo "Tx details:"
    gurud_cmd query tx "$txhash" -o json | jq '.tx.auth_info.fee'
    die "fee granter mismatch: expected=$expected_granter got=$granter_in_tx txhash=$txhash"
  fi
}

assert_delta() {
  local label="$1"
  local before="$2"
  local after="$3"
  local expected_delta="$4"

  python3 - "$label" "$before" "$after" "$expected_delta" <<'PY'
import sys

label, before_s, after_s, delta_s = sys.argv[1:5]
before = int(before_s)
after = int(after_s)
expected = int(delta_s)
actual = after - before
if actual != expected:
    print(f"ASSERT FAIL ({label}): before={before} after={after} actual_delta={actual} expected_delta={expected}", file=sys.stderr)
    sys.exit(1)
PY
}

eth_balance_wei() {
  local addr0x="$1"
  curl -sf -H 'Content-Type: application/json' \
    --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBalance\",\"params\":[\"${addr0x}\",\"latest\"],\"id\":1}" \
    "$ETH_RPC" \
    | jq -r '.result' \
    | python3 -c 'import sys; print(int(sys.stdin.read().strip(), 16))'
}

run_tx_and_get_hash() {
  # Runs a gurud tx ... command (args passed) and returns txhash.
  local out
  local code
  local txhash
  local attempt
  for ((attempt=1; attempt<=TX_RETRIES; attempt++)); do
    out="$(gurud_cmd "$@" -y -o json \
      --gas "$GAS" \
      --gas-adjustment "$GAS_ADJUSTMENT" \
      --gas-prices "$GAS_PRICES")"

    code="$(echo "$out" | jq -r '.code // 0')"
    if [[ "$code" == "0" ]]; then
      txhash="$(echo "$out" | jq -r '.txhash // empty')"
      [[ -n "$txhash" ]] || die "missing txhash in broadcast response"
      echo "$txhash"
      return 0
    fi

    # code=4 is typically sdk.ErrUnauthorized (often transient during startup / state load).
    if [[ "$code" == "4" && "$attempt" -lt "$TX_RETRIES" ]]; then
      sleep 1
      continue
    fi

    echo "$out" | jq .
    die "tx broadcast failed (code=$code)"
  done

  die "tx broadcast failed after retries"
}

start_local_node() {
  local args=()
  if [[ "$RESET_CHAIN" == "1" ]]; then
    args+=("-y")
  else
    # If home directory doesn't exist, force init
    if [[ ! -d "$HOME_DIR" ]]; then
      args+=("-y")
    else
      args+=("-n")
    fi
  fi
  if [[ "$SKIP_INSTALL" == "1" ]]; then
    args+=("--no-install")
  fi

  echo "Starting local node via ./local_node.sh ${args[*]} (home=${HOME_DIR})"
  (cd "$REPO_ROOT" && ./local_node.sh "${args[@]}") >/tmp/guru-local-node.log 2>&1 &
  LOCAL_NODE_PID=$!
  export LOCAL_NODE_PID

  # Best-effort: capture child gurud PID so we can stop it reliably.
  GURUD_PID=""
  local i
  for ((i=0; i<40; i++)); do
    if ! kill -0 "$LOCAL_NODE_PID" >/dev/null 2>&1; then
      die "local_node.sh exited early (check /tmp/guru-local-node.log)"
    fi
    # NOTE: on macOS, parent/child relationships can be unreliable for backgrounded scripts.
    # Prefer matching by command line.
    GURUD_PID="$(pgrep -f "gurud start .*--home ${HOME_DIR} .*--chain-id ${CHAIN_ID}" 2>/dev/null | awk 'NR==1{print;exit}' || true)"
    if [[ -n "$GURUD_PID" ]]; then
      break
    fi
    sleep 0.25
  done
  export GURUD_PID
}

cleanup() {
  if [[ -n "${LOCAL_NODE_PID:-}" ]]; then
    echo "Stopping local node (pid=${LOCAL_NODE_PID}${GURUD_PID:+, gurud=${GURUD_PID}})"

    if [[ -n "${GURUD_PID:-}" ]]; then
      kill "${GURUD_PID}" >/dev/null 2>&1 || true
    fi
    kill "${LOCAL_NODE_PID}" >/dev/null 2>&1 || true

    # Ensure nothing is left holding the database.
    sleep 1
    if [[ -n "${GURUD_PID:-}" ]]; then
      kill -9 "${GURUD_PID}" >/dev/null 2>&1 || true
    fi
    pkill -TERM -f "gurud start .*--home ${HOME_DIR} .*--chain-id ${CHAIN_ID}" >/dev/null 2>&1 || true
    sleep 1
    pkill -KILL -f "gurud start .*--home ${HOME_DIR} .*--chain-id ${CHAIN_ID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

usage() {
  cat <<EOF
Usage:
  scripts/test-feegrant.sh [flags]

Flags (env):
  START_NODE=auto|yes|no     (default: auto)
  RESET_CHAIN=0|1            (default: 0)
  SKIP_INSTALL=0|1           (default: 0)
  ONLY=all|cosmos|evm        (default: all)

  CHAIN_ID=$CHAIN_ID
  DENOM=$DENOM
  HOME_DIR=$HOME_DIR
  NODE_RPC=$NODE_RPC
  KEYRING_BACKEND=$KEYRING_BACKEND

  GRANTER_KEY=$GRANTER_KEY
  GRANTEE_KEY=$GRANTEE_KEY
  RECIPIENT_KEY=$RECIPIENT_KEY

  SPEND_LIMIT=$SPEND_LIMIT
  COSMOS_SEND_AMOUNT=$COSMOS_SEND_AMOUNT
  EVM_SEND_AMOUNT=$EVM_SEND_AMOUNT
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

need_cmd gurud
need_cmd jq
need_cmd curl
need_cmd python3
need_cmd awk
need_cmd grep
need_cmd head
need_cmd sed
need_cmd pgrep
need_cmd pkill
need_cmd tr

if [[ "$START_NODE" == "yes" ]]; then
  start_local_node
elif [[ "$START_NODE" == "auto" ]]; then
  if status_ok; then
    echo "Node is already running at ${NODE_RPC}"
  else
    start_local_node
  fi
elif [[ "$START_NODE" == "no" ]]; then
  :
else
  die "invalid START_NODE: $START_NODE (expected auto|yes|no)"
fi

echo "Waiting for node RPC to be ready..."
wait_for_node 90 || die "node RPC not ready: ${NODE_RPC} (check /tmp/guru-local-node.log)"
if [[ "$ONLY" == "all" || "$ONLY" == "evm" ]]; then
  echo "Waiting for ETH JSON-RPC to be ready..."
  wait_for_eth_rpc 90 || die "ETH JSON-RPC not ready: ${ETH_RPC}"
fi

GRANTER_ADDR="$(addr_bech32 "$GRANTER_KEY")"
GRANTEE_ADDR="$(addr_bech32 "$GRANTEE_KEY")"
RECIPIENT_ADDR="$(addr_bech32 "$RECIPIENT_KEY")"

GRANTEE_HEX="$(addr_hex "$GRANTEE_ADDR" || true)"
RECIPIENT_HEX="$(addr_hex "$RECIPIENT_ADDR" || true)"

echo "Using accounts:"
echo "  granter:  ${GRANTER_KEY} ${GRANTER_ADDR}"
echo "  grantee:  ${GRANTEE_KEY} ${GRANTEE_ADDR} ${GRANTEE_HEX:-}"
echo "  recipient:${RECIPIENT_KEY} ${RECIPIENT_ADDR} ${RECIPIENT_HEX:-}"
echo

if [[ "$ONLY" == "all" || "$ONLY" == "evm" ]]; then
  [[ -n "${GRANTEE_HEX:-}" ]] || die "failed to derive EVM hex address for grantee: ${GRANTEE_ADDR}"
  [[ -n "${RECIPIENT_HEX:-}" ]] || die "failed to derive EVM hex address for recipient: ${RECIPIENT_ADDR}"
fi

echo "Creating feegrant:"
if [[ -n "${SPEND_LIMIT:-}" ]]; then
  echo "  spend_limit=${SPEND_LIMIT}"
else
  echo "  spend_limit=unlimited"
fi

need_regrant="1"
if grant_exists "$GRANTER_ADDR" "$GRANTEE_ADDR"; then
  existing="$(gurud_cmd query feegrant grant "$GRANTER_ADDR" "$GRANTEE_ADDR" -o json)"
  existing_len="$(echo "$existing" | jq -r '(.allowance.allowance.value.spend_limit // []) | length')"
  existing_amt="$(echo "$existing" | jq -r '(.allowance.allowance.value.spend_limit // [])[0].amount // empty')"
  existing_denom="$(echo "$existing" | jq -r '(.allowance.allowance.value.spend_limit // [])[0].denom // empty')"

  if [[ -z "${SPEND_LIMIT:-}" ]]; then
    # Desired: unlimited (empty spend_limit)
    if [[ "$existing_len" == "0" ]]; then
      need_regrant="0"
      echo "Existing unlimited feegrant found. Reusing."
    fi
  else
    # Desired: explicit spend limit
    desired_amt="$(echo "$SPEND_LIMIT" | sed -E "s/${DENOM}\$//")"
    if [[ "$existing_len" == "1" && "$existing_amt" == "$desired_amt" && "$existing_denom" == "$DENOM" ]]; then
      need_regrant="0"
      echo "Existing matching feegrant found. Reusing."
    fi
  fi
fi

if [[ "$need_regrant" == "1" ]]; then
  if grant_exists "$GRANTER_ADDR" "$GRANTEE_ADDR"; then
    echo "Existing feegrant found but does not match desired allowance. Revoking first..."
    revoke_txhash="$(run_tx_and_get_hash tx feegrant revoke "$GRANTER_KEY" "$GRANTEE_ADDR")"
    wait_for_tx_success "$revoke_txhash" 90 || die "feegrant revoke tx not found: $revoke_txhash"
  fi

  grant_cmd=(tx feegrant grant "$GRANTER_KEY" "$GRANTEE_ADDR")
  if [[ -n "${SPEND_LIMIT:-}" ]]; then
    grant_cmd+=(--spend-limit "$SPEND_LIMIT")
  fi
  grant_txhash="$(run_tx_and_get_hash "${grant_cmd[@]}")"
  [[ -n "$grant_txhash" ]] || die "failed to get txhash from feegrant grant"
  wait_for_tx_success "$grant_txhash" 90 || die "feegrant grant tx not found: $grant_txhash"
fi

echo "Feegrant created. Verifying on-chain grant exists..."
gurud_cmd query feegrant grant "$GRANTER_ADDR" "$GRANTEE_ADDR" -o json | jq .
echo

if [[ "$ONLY" == "all" || "$ONLY" == "cosmos" ]]; then
  echo "=== Cosmos Tx test (bank send with --fee-granter) ==="

  b_granter_before="$(balance_amount "$GRANTER_ADDR")"
  b_grantee_before="$(balance_amount "$GRANTEE_ADDR")"
  b_recipient_before="$(balance_amount "$RECIPIENT_ADDR")"

  echo "Balances before:"
  echo "  granter   ${b_granter_before}${DENOM}"
  echo "  grantee   ${b_grantee_before}${DENOM}"
  echo "  recipient ${b_recipient_before}${DENOM}"

  cosmos_txhash="$(run_tx_and_get_hash tx bank send "$GRANTEE_KEY" "$RECIPIENT_ADDR" "$COSMOS_SEND_AMOUNT" --fee-granter "$GRANTER_ADDR")"
  [[ -n "$cosmos_txhash" ]] || die "failed to get txhash from cosmos bank send"
  wait_for_tx_success "$cosmos_txhash" 90 || die "cosmos tx not found: $cosmos_txhash"
  assert_fee_granter "$cosmos_txhash" "$GRANTER_ADDR"

  b_granter_after="$(balance_amount "$GRANTER_ADDR")"
  b_grantee_after="$(balance_amount "$GRANTEE_ADDR")"
  b_recipient_after="$(balance_amount "$RECIPIENT_ADDR")"

  echo "Balances after:"
  echo "  granter   ${b_granter_after}${DENOM}"
  echo "  grantee   ${b_grantee_after}${DENOM}"
  echo "  recipient ${b_recipient_after}${DENOM}"

  # Validate value transfer happened (fees are validated via fee.granter and granter balance going down).
  send_amt_int="$(echo "$COSMOS_SEND_AMOUNT" | sed -E "s/${DENOM}\$//")"
  assert_delta "cosmos recipient balance" "$b_recipient_before" "$b_recipient_after" "$send_amt_int"
  assert_delta "cosmos grantee balance (value only)" "$b_grantee_before" "$b_grantee_after" "-${send_amt_int}"

  python3 - "$b_granter_before" "$b_granter_after" <<'PY'
import sys
b0 = int(sys.argv[1]); b1 = int(sys.argv[2])
if b1 >= b0:
    print(f"ASSERT FAIL (cosmos granter balance): expected decrease, before={b0} after={b1}", file=sys.stderr)
    sys.exit(1)
PY

  echo "Cosmos Tx OK: txhash=${cosmos_txhash}"
  echo
fi

if [[ "$ONLY" == "all" || "$ONLY" == "evm" ]]; then
  echo "=== EVM Tx test (evm send with --fee-granter) ==="

  b_granter_before="$(balance_amount "$GRANTER_ADDR")"
  b_grantee_before="$(balance_amount "$GRANTEE_ADDR")"
  b_recipient_before="$(balance_amount "$RECIPIENT_ADDR")"
  e_grantee_before="$(eth_balance_wei "$GRANTEE_HEX")"
  e_recipient_before="$(eth_balance_wei "$RECIPIENT_HEX")"

  echo "Balances before:"
  echo "  granter   ${b_granter_before}${DENOM}"
  echo "  grantee   ${b_grantee_before}${DENOM}"
  echo "  recipient ${b_recipient_before}${DENOM}"
  echo "ETH balances before (wei):"
  echo "  grantee   ${e_grantee_before}"
  echo "  recipient ${e_recipient_before}"

  # Use hex address if available to make it obvious it's an ETH-style transfer.
  to_addr="${RECIPIENT_HEX:-$RECIPIENT_ADDR}"

  evm_txhash="$(run_tx_and_get_hash tx evm send "$GRANTEE_KEY" "$to_addr" "$EVM_SEND_AMOUNT" --fee-granter "$GRANTER_ADDR")"
  [[ -n "$evm_txhash" ]] || die "failed to get txhash from evm send"
  wait_for_tx_success "$evm_txhash" 90 || die "evm tx not found: $evm_txhash"
  assert_fee_granter "$evm_txhash" "$GRANTER_ADDR"

  b_granter_after="$(balance_amount "$GRANTER_ADDR")"
  b_grantee_after="$(balance_amount "$GRANTEE_ADDR")"
  b_recipient_after="$(balance_amount "$RECIPIENT_ADDR")"
  e_grantee_after="$(eth_balance_wei "$GRANTEE_HEX")"
  e_recipient_after="$(eth_balance_wei "$RECIPIENT_HEX")"

  echo "Balances after:"
  echo "  granter   ${b_granter_after}${DENOM}"
  echo "  grantee   ${b_grantee_after}${DENOM}"
  echo "  recipient ${b_recipient_after}${DENOM}"
  echo "ETH balances after (wei):"
  echo "  grantee   ${e_grantee_after}"
  echo "  recipient ${e_recipient_after}"

  send_amt_int="$(echo "$EVM_SEND_AMOUNT" | sed -E "s/${DENOM}\$//")"
  # Bank balance assertions: sender should lose the value; recipient bank balance may not change depending on EVM integration.
  assert_delta "evm grantee bank balance (value only)" "$b_grantee_before" "$b_grantee_after" "-${send_amt_int}"
  # ETH balance assertions: recipient must gain the value.
  assert_delta "evm recipient eth balance (wei)" "$e_recipient_before" "$e_recipient_after" "$send_amt_int"

  python3 - "$b_granter_before" "$b_granter_after" <<'PY'
import sys
b0 = int(sys.argv[1]); b1 = int(sys.argv[2])
if b1 >= b0:
    print(f"ASSERT FAIL (evm granter balance): expected decrease, before={b0} after={b1}", file=sys.stderr)
    sys.exit(1)
PY

  echo "EVM Tx OK: txhash=${evm_txhash}"
  echo
fi

echo "All requested tests passed."

