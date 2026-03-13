#!/usr/bin/env bash

set -euo pipefail

RPC_URL="${RPC_URL:-http://127.0.0.1:8545}"
REST_URL="${REST_URL:-http://127.0.0.1:1317}"
GRPC_ADDR="${GRPC_ADDR:-127.0.0.1:9090}"
HOME_DIR="${HOME_DIR:-$HOME/.gurud}"
CHAIN_ID="${CHAIN_ID:-guru_631-1}"
FROM_KEY="${FROM_KEY:-dev0}"
TO_KEY="${TO_KEY:-dev1}"
DENOM="${DENOM:-agxn}"
DEV0_MNEMONIC="${DEV0_MNEMONIC:-copper push brief egg scan entry inform record adjust fossil boss egg comic alien upon aspect dry avoid interest fury window hint race symptom}"
PRIVATE_KEY="${PRIVATE_KEY:-}"
TO_HEX="${TO_HEX:-}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_cmd curl
require_cmd jq
require_cmd cast
require_cmd solc
require_cmd grpcurl
require_cmd gurud

echo "[1/7] Checking endpoints..."
curl -fsS "${REST_URL}/cosmos/base/tendermint/v1beta1/blocks/latest" >/dev/null
curl -fsS -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}' \
  "${RPC_URL}" >/dev/null

FROM_HEX="$(curl -fsS -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"personal_listAccounts","params":[],"id":2}' \
  "${RPC_URL}" | jq -r '.result[0]')"
TO_HEX_FROM_RPC="$(curl -fsS -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"personal_listAccounts","params":[],"id":3}' \
  "${RPC_URL}" | jq -r '.result[1]')"

if [[ -z "${PRIVATE_KEY}" ]]; then
  PRIVATE_KEY="$(cast wallet private-key --mnemonic "${DEV0_MNEMONIC}")"
fi
FROM_HEX="$(cast wallet address --private-key "${PRIVATE_KEY}")"

if [[ -z "${TO_HEX}" ]]; then
  if [[ -n "${TO_HEX_FROM_RPC}" && "${TO_HEX_FROM_RPC}" != "null" ]]; then
    TO_HEX="${TO_HEX_FROM_RPC}"
  else
    # Fallback to static dev1 address used by local_node.sh
    TO_HEX="0x963ebdf2e1f8db8707d05fc75bfeffba1b5bac17"
  fi
fi

send_cast() {
  local output=""
  local rc=0
  local i=0
  local max_attempts=3
  while [[ ${i} -lt ${max_attempts} ]]; do
    i=$((i + 1))
    set +e
    output="$(cast send --rpc-url "${RPC_URL}" --private-key "${PRIVATE_KEY}" "$@" --json 2>&1)"
    rc=$?
    set -e
    if [[ ${rc} -eq 0 ]]; then
      echo "${output}"
      return 0
    fi

    # Intermittent null response can happen right after block transitions.
    if [[ "${output}" == *"server returned a null response"* && ${i} -lt ${max_attempts} ]]; then
      sleep 1
      continue
    fi

    echo "${output}" >&2
    return 1
  done

  echo "${output}" >&2
  return 1
}

echo "[2/7] Deploying and calling Solidity contract..."
BYTECODE="$(solc --optimize --bin - <<'EOF' | awk 'BEGIN{found=0} /Binary:/{found=1;next} found==1 && NF{print; exit}'
// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;
contract Counter {
    uint256 public value;
    function set(uint256 v) external { value = v; }
}
EOF
)"

DEPLOY_JSON="$(send_cast \
  --legacy --gas-price 700000000000 --gas-limit 2000000 --create "${BYTECODE}")"
CONTRACT_ADDR="$(echo "${DEPLOY_JSON}" | jq -r '.contractAddress')"
if [[ -z "${CONTRACT_ADDR}" || "${CONTRACT_ADDR}" == "null" ]]; then
  echo "contract deployment failed: ${DEPLOY_JSON}" >&2
  exit 1
fi

send_cast \
  --legacy --gas-price 700000000000 --gas-limit 200000 \
  "${CONTRACT_ADDR}" "set(uint256)" 42 >/dev/null
VALUE_NOW="$(cast call --rpc-url "${RPC_URL}" "${CONTRACT_ADDR}" "value()(uint256)")"
if [[ "${VALUE_NOW}" != "42" ]]; then
  echo "contract call regression: expected 42 got ${VALUE_NOW}" >&2
  exit 1
fi

echo "[3/7] Sending EIP-1559 transaction..."
EIP1559_JSON="$(send_cast \
  --gas-price 1200000000000 --priority-gas-price 700000000000 \
  --gas-limit 21000 "${TO_HEX}" --value 0.005ether)"
EIP1559_HASH="$(echo "${EIP1559_JSON}" | jq -r '.transactionHash')"
if [[ -z "${EIP1559_HASH}" || "${EIP1559_HASH}" == "null" ]]; then
  echo "eip-1559 transaction failed: ${EIP1559_JSON}" >&2
  exit 1
fi

TX_BY_HASH="$(curl -fsS -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getTransactionByHash","params":["'"${EIP1559_HASH}"'"],"id":4}' \
  "${RPC_URL}")"
TX_TYPE="$(echo "${TX_BY_HASH}" | jq -r '.result.type')"
if [[ "${TX_TYPE}" != "0x2" ]]; then
  echo "expected EIP-1559 tx type 0x2, got ${TX_TYPE}" >&2
  exit 1
fi

echo "[4/7] Sending Cosmos bank tx for gRPC tx lookup..."
TO_COSMOS="$(gurud keys show "${TO_KEY}" --address --home "${HOME_DIR}" --keyring-backend test)"
SEND_JSON="$(gurud tx bank send "${FROM_KEY}" "${TO_COSMOS}" 10000000000000000"${DENOM}" \
  --home "${HOME_DIR}" --keyring-backend test --chain-id "${CHAIN_ID}" \
  --fees 20000000000000000"${DENOM}" -y --output json)"
SEND_HASH="$(echo "${SEND_JSON}" | jq -r '.txhash')"
if [[ -z "${SEND_HASH}" || "${SEND_HASH}" == "null" ]]; then
  echo "cosmos bank send failed: ${SEND_JSON}" >&2
  exit 1
fi

echo "[5/7] Querying gRPC Balance..."
BAL_JSON="$(grpcurl -plaintext \
  -d '{"address":"'"${TO_COSMOS}"'","denom":"'"${DENOM}"'"}' \
  "${GRPC_ADDR}" cosmos.bank.v1beta1.Query/Balance)"
BAL_AMOUNT="$(echo "${BAL_JSON}" | jq -r '.balance.amount')"
if [[ -z "${BAL_AMOUNT}" || "${BAL_AMOUNT}" == "null" ]]; then
  echo "grpc balance query failed: ${BAL_JSON}" >&2
  exit 1
fi

echo "[6/7] Querying gRPC GetTx with retry..."
GETTX_JSON=""
for i in $(seq 1 20); do
  if GETTX_JSON="$(grpcurl -plaintext -d '{"hash":"'"${SEND_HASH}"'"}' \
      "${GRPC_ADDR}" cosmos.tx.v1beta1.Service/GetTx 2>/dev/null)"; then
    break
  fi
  sleep 1
done
if [[ -z "${GETTX_JSON}" ]]; then
  echo "grpc gettx failed for hash: ${SEND_HASH}" >&2
  exit 1
fi
GETTX_HASH="$(echo "${GETTX_JSON}" | jq -r '.txResponse.txhash')"
if [[ "${GETTX_HASH}" != "${SEND_HASH}" ]]; then
  echo "grpc gettx hash mismatch: expected ${SEND_HASH}, got ${GETTX_HASH}" >&2
  exit 1
fi
if echo "${GETTX_JSON}" | jq -e '.. | objects | select(has("@error"))' >/dev/null 2>&1; then
  echo "warning: unresolved Any type detected in GetTx JSON payload" >&2
fi

echo "[7/7] Final summary"
echo "contract=${CONTRACT_ADDR}"
echo "eip1559_hash=${EIP1559_HASH}"
echo "cosmos_tx_hash=${SEND_HASH}"
echo "grpc_balance=${BAL_AMOUNT}${DENOM}"
echo "smoke_status=PASS"
