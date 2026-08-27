#!/usr/bin/env bash

set -euo pipefail

DOCKER_BIN="${DOCKER:-docker}"
IMAGE="${1:-guru-node:local}"
NODE_HOME="/var/lib/guru/.gurud"
CHAIN_ID="guru-docker-smoke-1"
EVM_CHAIN_ID="9631"
MIN_GAS_PRICE="630000000000"
RESOURCE_SUFFIX="$(date +%s)-$$"
CONTAINER_NAME="guru-docker-smoke-${RESOURCE_SUFFIX}"
VOLUME_NAME="guru-docker-smoke-data-${RESOURCE_SUFFIX}"

cleanup() {
  status=$?

  if [[ $status -ne 0 ]] && "$DOCKER_BIN" container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    echo "Docker node smoke test failed; recent node logs:" >&2
    "$DOCKER_BIN" container logs --tail 200 "$CONTAINER_NAME" >&2 || true
  fi

  case "$CONTAINER_NAME" in
    guru-docker-smoke-*)
      "$DOCKER_BIN" container rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || true
      ;;
  esac
  case "$VOLUME_NAME" in
    guru-docker-smoke-data-*)
      "$DOCKER_BIN" volume rm "$VOLUME_NAME" >/dev/null 2>&1 || true
      ;;
  esac
}
trap cleanup EXIT

run_gurud() {
  "$DOCKER_BIN" run --rm \
    --volume "$VOLUME_NAME:$NODE_HOME" \
    "$IMAGE" "$@" --home "$NODE_HOME"
}

current_height() {
  status_json="$($DOCKER_BIN exec "$CONTAINER_NAME" \
    gurud status --node tcp://127.0.0.1:26657 2>/dev/null || true)"
  printf '%s\n' "$status_json" \
    | sed -n 's/.*"latest_block_height":"\([0-9][0-9]*\)".*/\1/p'
}

wait_for_height_greater_than() {
  threshold="$1"
  attempt=1
  while [[ $attempt -le 60 ]]; do
    height="$(current_height)"
    if [[ -n "$height" ]] && [[ "$height" -gt "$threshold" ]]; then
      printf '%s\n' "$height"
      return 0
    fi
    sleep 2
    attempt=$((attempt + 1))
  done

  echo "node did not commit a block above height $threshold" >&2
  return 1
}

command -v "$DOCKER_BIN" >/dev/null 2>&1 || {
  echo "Docker is required" >&2
  exit 1
}
"$DOCKER_BIN" image inspect "$IMAGE" >/dev/null 2>&1 || {
  echo "Docker image not found: $IMAGE (run 'make docker-build' first)" >&2
  exit 1
}

"$DOCKER_BIN" volume create \
  --label io.gurufin.guru.test=ephemeral \
  "$VOLUME_NAME" >/dev/null

echo "Initializing an ephemeral Guru network in Docker..."
run_gurud keys add constitution-base \
  --algo eth_secp256k1 --keyring-backend test >/dev/null 2>&1 || {
    echo "failed to create the Constitution base development key" >&2
    exit 1
  }
run_gurud keys add constitution-moderator \
  --algo eth_secp256k1 --keyring-backend test >/dev/null 2>&1 || {
    echo "failed to create the Constitution moderator development key" >&2
    exit 1
  }

constitution_base_address="$(run_gurud keys show constitution-base \
  --address --keyring-backend test)"
constitution_moderator_address="$(run_gurud keys show constitution-moderator \
  --address --keyring-backend test)"

run_gurud init docker-validator \
  --chain-id "$CHAIN_ID" \
  --constitution-base-address "$constitution_base_address" \
  --constitution-moderator-address "$constitution_moderator_address" >/dev/null

run_gurud keys add validator \
  --algo eth_secp256k1 --keyring-backend test >/dev/null 2>&1 || {
    echo "failed to create the validator development key" >&2
    exit 1
  }
validator_address="$(run_gurud keys show validator \
  --address --keyring-backend test)"

run_gurud genesis add-genesis-account \
  "$validator_address" 1000000000000000000000agxn >/dev/null
run_gurud genesis add-genesis-account \
  "$constitution_base_address" 100000000000000000000agxn >/dev/null
run_gurud genesis add-genesis-account \
  "$constitution_moderator_address" 100000000000000000000agxn >/dev/null
run_gurud genesis gentx validator 1000000000000000000agxn \
  --chain-id "$CHAIN_ID" \
  --gas 200000 \
  --gas-prices "${MIN_GAS_PRICE}agxn" \
  --keyring-backend test >/dev/null 2>&1
run_gurud genesis collect-gentxs >/dev/null 2>&1
run_gurud genesis validate >/dev/null
run_gurud config set app oracle.enabled false >/dev/null

mempool_max_txs="$(run_gurud config get app mempool.max-txs)"
[[ "$mempool_max_txs" == "-1" ]] || {
  echo "expected app mempool.max-txs=-1, got: $mempool_max_txs" >&2
  exit 1
}

node_id_before="$(run_gurud comet show-node-id)"

"$DOCKER_BIN" run --detach \
  --name "$CONTAINER_NAME" \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --volume "$VOLUME_NAME:$NODE_HOME" \
  "$IMAGE" start \
    --home "$NODE_HOME" \
    --chain-id "$CHAIN_ID" \
    --evm.evm-chain-id "$EVM_CHAIN_ID" \
    --minimum-gas-prices "${MIN_GAS_PRICE}agxn" \
    --p2p.laddr tcp://127.0.0.1:26656 >/dev/null

height_before_restart="$(wait_for_height_greater_than 1)"
"$DOCKER_BIN" stop --time 120 "$CONTAINER_NAME" >/dev/null
"$DOCKER_BIN" start "$CONTAINER_NAME" >/dev/null
height_after_restart="$(wait_for_height_greater_than "$height_before_restart")"
node_id_after="$("$DOCKER_BIN" exec "$CONTAINER_NAME" \
  gurud comet show-node-id --home "$NODE_HOME")"

[[ "$node_id_before" == "$node_id_after" ]] || {
  echo "node ID changed after restart" >&2
  exit 1
}

printf 'Docker node smoke test passed: height %s -> %s, node ID %s\n' \
  "$height_before_restart" "$height_after_restart" "$node_id_after"
