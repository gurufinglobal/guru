#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_BINARY="$SCRIPT_DIR/build/gurud"

CHAIN_ID="${CHAIN_ID:-guru_631-1}"
EVM_CHAIN_ID="${EVM_CHAIN_ID:-631}"
MONIKER="${MONIKER:-localtestnet}"
CHAIN_HOME="${GURU_HOME:-${HOME:?HOME is not set}/.gurud}"
BOND_DENOM="${BOND_DENOM:-agxn}"
DISPLAY_DENOM="${DISPLAY_DENOM:-gxn}"
KEYRING_BACKEND="test"
KEY_ALGORITHM="eth_secp256k1"
LOG_LEVEL="${LOG_LEVEL:-info}"
BASE_FEE="${BASE_FEE:-10000000}"
BUILD_GOTOOLCHAIN="${GURU_GOTOOLCHAIN:-go1.23.8}"

CONFIG_TOML="$CHAIN_HOME/config/config.toml"
APP_TOML="$CHAIN_HOME/config/app.toml"
GENESIS="$CHAIN_HOME/config/genesis.json"
TMP_GENESIS="$CHAIN_HOME/config/genesis.json.tmp"

build_binary=true
overwrite=""
build_for_debug=false
additional_users=0
mnemonic_file=""
mnemonics_input=""

usage() {
  cat <<EOF
Usage: $0 [options]

Options:
  -y                       Overwrite existing chain data without prompting
  -n                       Keep existing chain data and start the node
  --no-install             Skip building gurud (compatible with the reference script)
  --no-build               Alias for --no-install
  --remote-debugging       Build gurud with compiler optimizations disabled
  --additional-users N     Create N extra users after the four default users
  --mnemonic-file PATH     Write generated mnemonics to PATH
                           (default: \$GURU_HOME/mnemonics.yaml)
  --mnemonics-input PATH   Read all dev mnemonics from a simple YAML list
  -h, --help               Show this help

Environment:
  GURUD_BINARY             Use this gurud binary and skip the default build
  GURU_HOME                Node home (default: \$HOME/.gurud)
  CHAIN_ID                 Cosmos chain ID (default: guru_631-1)
  EVM_CHAIN_ID             EIP-155 chain ID (default: 631)
  BOND_DENOM               Native base denomination (default: agxn)
  DISPLAY_DENOM            Native display denomination (default: gxn)
  GURU_GOTOOLCHAIN         Go toolchain used to build (default: go1.23.8)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -y)
      overwrite="y"
      shift
      ;;
    -n)
      overwrite="n"
      shift
      ;;
    --no-install | --no-build)
      build_binary=false
      shift
      ;;
    --remote-debugging)
      build_for_debug=true
      shift
      ;;
    --additional-users)
      if [[ ! "${2:-}" =~ ^[0-9]+$ ]]; then
        echo >&2 "Error: --additional-users requires a non-negative integer."
        usage >&2
        exit 1
      fi
      additional_users="$2"
      shift 2
      ;;
    --mnemonic-file)
      if [[ -z "${2:-}" || "$2" == -* ]]; then
        echo >&2 "Error: --mnemonic-file requires a path."
        usage >&2
        exit 1
      fi
      mnemonic_file="$2"
      shift 2
      ;;
    --mnemonics-input)
      if [[ -z "${2:-}" || "$2" == -* ]]; then
        echo >&2 "Error: --mnemonics-input requires a path."
        usage >&2
        exit 1
      fi
      mnemonics_input="$2"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo >&2 "Error: unknown option: $1"
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -n "$mnemonics_input" && "$additional_users" -gt 0 ]]; then
  echo >&2 "Error: --mnemonics-input and --additional-users cannot be used together."
  exit 1
fi

if [[ ! "$BUILD_GOTOOLCHAIN" =~ ^(auto|local|path|go[0-9]+\.[0-9]+(\.[0-9]+)?([+-](auto|path))?)$ ]]; then
  echo >&2 "Error: unsupported GURU_GOTOOLCHAIN value: $BUILD_GOTOOLCHAIN"
  exit 1
fi

command -v jq >/dev/null 2>&1 || {
  echo >&2 "jq is required. See https://jqlang.github.io/jq/download/"
  exit 1
}

if [[ -n "${GURUD_BINARY:-}" ]]; then
  build_binary=false
  GURUD="$GURUD_BINARY"
else
  GURUD="$DEFAULT_BINARY"
fi

if [[ "$build_binary" == true ]]; then
  if [[ "$build_for_debug" == true ]]; then
    mkdir -p "$SCRIPT_DIR/build"
    (
      cd "$SCRIPT_DIR"
      env GOTOOLCHAIN="$BUILD_GOTOOLCHAIN" \
        go build -mod=readonly -gcflags='all=-N -l' -o "$DEFAULT_BINARY" ./cmd/gurud
    )
  else
    make -C "$SCRIPT_DIR" build \
      "GO=env GOTOOLCHAIN=$BUILD_GOTOOLCHAIN go"
  fi
elif [[ ! -x "$GURUD" ]]; then
  if command -v gurud >/dev/null 2>&1; then
    GURUD="$(command -v gurud)"
  else
    echo >&2 "Error: gurud is not executable at $GURUD. Build it or set GURUD_BINARY."
    exit 1
  fi
fi

if [[ ! -x "$GURUD" ]]; then
  echo >&2 "Error: gurud is not executable at $GURUD."
  exit 1
fi

if [[ -z "$overwrite" ]]; then
  if [[ -d "$CHAIN_HOME" ]]; then
    printf '\nExisting local node data was found at %s.\n' "$CHAIN_HOME"
    printf 'Overwrite it and create fresh genesis data? [y/n] '
    read -r overwrite
  else
    overwrite="y"
  fi
fi

read_mnemonics_yaml() {
  local file_path="$1"

  awk '
    BEGIN { in_list=0 }
    /^[[:space:]]*mnemonics:[[:space:]]*$/ { in_list=1; next }
    in_list && /^[[:space:]]*-[[:space:]]*/ {
      line=$0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      gsub(/^"[[:space:]]*|[[:space:]]*"$/, "", line)
      gsub(/^'\''[[:space:]]*|[[:space:]]*'\''$/, "", line)
      print line
      next
    }
    in_list && NF==0 { next }
  ' "$file_path"
}

write_mnemonics_yaml() {
  local file_path="$1"
  shift
  local mnemonic

  mkdir -p "$(dirname "$file_path")"
  umask 077
  {
    echo "mnemonics:"
    for mnemonic in "$@"; do
      printf '  - "%s"\n' "$mnemonic"
    done
  } >"$file_path"
  echo "Wrote mnemonics to $file_path"
}

add_genesis_funds() {
  local key_name="$1"

  "$GURUD" genesis add-genesis-account \
    "$key_name" "1000000000000000000000${BOND_DENOM}" \
    --keyring-backend "$KEYRING_BACKEND" \
    --home "$CHAIN_HOME"
}

replace_config_value() {
  local file_path="$1"
  local expression="$2"

  sed -i.bak -e "$expression" "$file_path"
  rm -f -- "${file_path}.bak"
}

cleanup() {
  rm -f -- "$TMP_GENESIS"
}
trap cleanup EXIT

if [[ "$overwrite" == "y" || "$overwrite" == "Y" ]]; then
  case "$CHAIN_HOME" in
    "" | "/" | "${HOME:?HOME is not set}")
      echo >&2 "Error: refusing to overwrite unsafe GURU_HOME: $CHAIN_HOME"
      exit 1
      ;;
  esac

  rm -rf -- "$CHAIN_HOME"

  "$GURUD" config set client chain-id "$CHAIN_ID" --home "$CHAIN_HOME"
  "$GURUD" config set client keyring-backend "$KEYRING_BACKEND" --home "$CHAIN_HOME"

  validator_key="mykey"
  validator_mnemonic="gesture inject test cycle original hollow east ridge hen combine junk child bacon zero hope comfort vacuum milk pitch cage oppose unhappy lunar seat"

  # Deterministic, public development accounts. Never use these keys on a
  # public network or fund them with assets of value.
  default_mnemonics=(
    "copper push brief egg scan entry inform record adjust fossil boss egg comic alien upon aspect dry avoid interest fury window hint race symptom"
    "maximum display century economy unlock van census kite error heart snow filter midnight usage egg venture cash kick motor survey drastic edge muffin visual"
    "will wear settle write dance topic tape sea glory hotel oppose rebel client problem era video gossip glide during yard balance cancel file rose"
    "doll midnight silk carpet brush boring pluck office gown inquiry duck chief aim exit gain never tennis crime fragile ship cloud surface exotic patch"
  )

  echo "$validator_mnemonic" | "$GURUD" keys add "$validator_key" \
    --recover \
    --keyring-backend "$KEYRING_BACKEND" \
    --algo "$KEY_ALGORITHM" \
    --home "$CHAIN_HOME"

  provided_mnemonics=()
  if [[ -n "$mnemonics_input" ]]; then
    if [[ ! -f "$mnemonics_input" ]]; then
      echo >&2 "Error: mnemonic input file not found: $mnemonics_input"
      exit 1
    fi

    while IFS= read -r mnemonic; do
      [[ -n "$mnemonic" ]] && provided_mnemonics+=("$mnemonic")
    done < <(read_mnemonics_yaml "$mnemonics_input")

    if [[ ${#provided_mnemonics[@]} -eq 0 ]]; then
      echo >&2 "Error: no entries found below 'mnemonics:' in $mnemonics_input"
      exit 1
    fi
  fi

  if [[ ${#provided_mnemonics[@]} -gt 0 ]]; then
    dev_mnemonics=("${provided_mnemonics[@]}")
  else
    dev_mnemonics=("${default_mnemonics[@]}")
  fi

  echo "$validator_mnemonic" | "$GURUD" init "$MONIKER" \
    --overwrite \
    --recover \
    --chain-id "$CHAIN_ID" \
    --default-denom "$BOND_DENOM" \
    --home "$CHAIN_HOME"

  jq \
    --arg bond_denom "$BOND_DENOM" \
    --arg display_denom "$DISPLAY_DENOM" '
      .app_state.staking.params.bond_denom = $bond_denom
      | .app_state.mint.params.mint_denom = $bond_denom
      | .app_state.evm.params.evm_denom = $bond_denom
      | if .app_state.evm.params.extended_denom_options != null then
          .app_state.evm.params.extended_denom_options.extended_denom = $bond_denom
        else . end
      | if .app_state.gov.params.min_deposit[0] != null then
          .app_state.gov.params.min_deposit[0].denom = $bond_denom
        else . end
      | if .app_state.gov.params.expedited_min_deposit[0] != null then
          .app_state.gov.params.expedited_min_deposit[0].denom = $bond_denom
        else . end
      | .app_state.bank.denom_metadata = [{
          description: "The native staking and EVM gas token of the Guru chain",
          denom_units: [
            {denom: $bond_denom, exponent: 0, aliases: ["atto\($display_denom)"]},
            {denom: $display_denom, exponent: 18, aliases: []}
          ],
          base: $bond_denom,
          display: $display_denom,
          name: "Guru",
          symbol: "GXN",
          uri: "",
          uri_hash: ""
        }]
      | .app_state.evm.params.active_static_precompiles = [
          "0x0000000000000000000000000000000000000100",
          "0x0000000000000000000000000000000000000400",
          "0x0000000000000000000000000000000000000800",
          "0x0000000000000000000000000000000000000801",
          "0x0000000000000000000000000000000000000802",
          "0x0000000000000000000000000000000000000804",
          "0x0000000000000000000000000000000000000805",
          "0x0000000000000000000000000000000000000806"
        ]
      | if .app_state.erc20 != null then
          .app_state.erc20.native_precompiles = [
            "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"
          ]
          | .app_state.erc20.token_pairs = [{
              contract_owner: 1,
              erc20_address: "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
              denom: $bond_denom,
              enabled: true
            }]
        else . end
      | .consensus.params.block.max_gas = "10000000"
      | if .app_state.gov.params.max_deposit_period != null then
          .app_state.gov.params.max_deposit_period = "30s"
        else . end
      | if .app_state.gov.params.voting_period != null then
          .app_state.gov.params.voting_period = "30s"
        else . end
      | if .app_state.gov.params.expedited_voting_period != null then
          .app_state.gov.params.expedited_voting_period = "15s"
        else . end
    ' "$GENESIS" >"$TMP_GENESIS"
  mv "$TMP_GENESIS" "$GENESIS"

  "$GURUD" genesis add-genesis-account \
    "$validator_key" "100000000000000000000000000${BOND_DENOM}" \
    --keyring-backend "$KEYRING_BACKEND" \
    --home "$CHAIN_HOME"

  replace_config_value "$CONFIG_TOML" 's/timeout_propose = "3s"/timeout_propose = "2s"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "200ms"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_prevote = "1s"/timeout_prevote = "500ms"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "200ms"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_precommit = "1s"/timeout_precommit = "500ms"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "200ms"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_commit = "5s"/timeout_commit = "500ms"/'
  replace_config_value "$CONFIG_TOML" 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "5s"/'
  replace_config_value "$CONFIG_TOML" 's/prometheus = false/prometheus = true/'

  replace_config_value "$APP_TOML" 's/^prometheus-retention-time[[:space:]]*=.*/prometheus-retention-time = 1000000000000/'
  replace_config_value "$APP_TOML" 's/enabled = false/enabled = true/g'
  replace_config_value "$APP_TOML" 's/enable = false/enable = true/g'

  final_mnemonics=("${dev_mnemonics[@]}")
  if [[ -z "$mnemonic_file" ]]; then
    mnemonic_file="$CHAIN_HOME/mnemonics.yaml"
  fi

  for ((i = 0; i < ${#dev_mnemonics[@]}; i++)); do
    key_name="dev${i}"
    mnemonic="${dev_mnemonics[i]}"
    echo "$mnemonic" | "$GURUD" keys add "$key_name" \
      --recover \
      --keyring-backend "$KEYRING_BACKEND" \
      --algo "$KEY_ALGORITHM" \
      --home "$CHAIN_HOME"
    add_genesis_funds "$key_name"
  done

  if [[ "$additional_users" -gt 0 ]]; then
    start_index=${#dev_mnemonics[@]}
    for ((i = 0; i < additional_users; i++)); do
      index=$((start_index + i))
      key_name="dev${index}"
      key_output="$(
        "$GURUD" keys add "$key_name" \
          --keyring-backend "$KEYRING_BACKEND" \
          --algo "$KEY_ALGORITHM" \
          --home "$CHAIN_HOME" 2>&1
      )"
      generated_mnemonic="$(
        printf '%s\n' "$key_output" |
          grep -E '([[:alpha:]]+[[:space:]]+){11,}[[:alpha:]]+$' |
          tail -n 1 ||
          true
      )"
      generated_mnemonic="${generated_mnemonic//$'\r'/}"
      if [[ -z "$generated_mnemonic" ]]; then
        echo >&2 "Error: failed to capture mnemonic for $key_name"
        exit 1
      fi

      final_mnemonics+=("$generated_mnemonic")
      add_genesis_funds "$key_name"
    done
    write_mnemonics_yaml "$mnemonic_file" "${final_mnemonics[@]}"
  fi

  "$GURUD" genesis gentx \
    "$validator_key" "1000000000000000000000${BOND_DENOM}" \
    --gas-prices "${BASE_FEE}${BOND_DENOM}" \
    --keyring-backend "$KEYRING_BACKEND" \
    --chain-id "$CHAIN_ID" \
    --home "$CHAIN_HOME"
  "$GURUD" genesis collect-gentxs --home "$CHAIN_HOME"
  "$GURUD" genesis validate --home "$CHAIN_HOME"
fi

start_args=(
  start
  --pruning nothing
  --log_level "$LOG_LEVEL"
  --minimum-gas-prices "0${BOND_DENOM}"
  --evm.min-tip 0
  --home "$CHAIN_HOME"
  --json-rpc.api eth,txpool,personal,net,debug,web3
  --evm.evm-chain-id "$EVM_CHAIN_ID"
  --chain-id "$CHAIN_ID"
)
if [[ -n "${TRACE:-}" ]]; then
  start_args+=("$TRACE")
fi

exec "$GURUD" "${start_args[@]}"
