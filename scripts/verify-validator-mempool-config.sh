#!/usr/bin/env bash

# Validate rendered validator configuration. This is not a startup hook and
# does not inspect CLI or service-environment overrides of a later process.

set -u
set -o pipefail

readonly schema="guru.validator-mempool-preflight/v2"
readonly exit_policy=1
readonly exit_error=2

home=""
validator_id=""
binary=""
output=""
scratch=""

usage() {
  printf '%s\n' \
    'Usage: verify-validator-mempool-config.sh --home PATH --validator-id ID' \
    '       --node-binary PATH [--output PATH]'
}

die() {
  printf 'preflight error: %s\n' "$*" >&2
  exit "$exit_error"
}

cleanup() {
  [[ -n "$scratch" && -d "$scratch" ]] || return 0
  case "$scratch" in
    */.validator-mempool-preflight.*) rm -rf "$scratch" ;;
  esac
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM

while [[ $# -gt 0 ]]; do
  case "$1" in
    --home|--validator-id|--node-binary|--output)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      case "$1" in
        --home) home=$2 ;;
        --validator-id) validator_id=$2 ;;
        --node-binary) binary=$2 ;;
        --output) output=$2 ;;
      esac
      shift 2
      ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$home" ]] || die "--home is required"
[[ -n "$validator_id" ]] || die "--validator-id is required"
[[ -n "$binary" ]] || die "--node-binary is required"
command -v jq >/dev/null 2>&1 || die "jq is required"
jq -n -e --arg id "$validator_id" '$id | test("\\S")' >/dev/null \
  || die "--validator-id must contain a non-whitespace character"

if command -v sha256sum >/dev/null 2>&1; then
  hash_command=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  hash_command=(shasum -a 256)
else
  die "sha256sum or shasum is required"
fi

absolute_file() {
  local directory
  directory=$(cd -P "$(dirname "$1")" 2>/dev/null && pwd -P) || return 1
  printf '%s/%s\n' "$directory" "$(basename "$1")"
}

home=$(cd -P "$home" 2>/dev/null && pwd -P) || die "home: cannot resolve directory"
app="$home/config/app.toml"
config="$home/config/config.toml"
genesis="$home/config/genesis.json"
for path in "$app" "$config" "$genesis"; do
  [[ -f "$path" ]] || die "input: expected regular file, got $path"
done
app=$(absolute_file "$app") || die "app.toml: cannot resolve path"
config=$(absolute_file "$config") || die "config.toml: cannot resolve path"
genesis=$(absolute_file "$genesis") || die "genesis.json: cannot resolve path"
binary=$(absolute_file "$binary") || die "node binary: cannot resolve path"
[[ -f "$binary" && -x "$binary" ]] || die "node binary: expected executable regular file, got $binary"

output_path=""
if [[ -n "$output" ]]; then
  [[ ! -L "$output" ]] || die "output: symlink targets are not allowed: $output"
  [[ ! -e "$output" || -f "$output" ]] || die "output: expected regular file target: $output"
  output_parent=$(dirname "$output")
  mkdir -p "$output_parent" || die "output: cannot create parent directory"
  output_parent=$(cd -P "$output_parent" && pwd -P) || die "output: cannot resolve parent directory"
  output_path="$output_parent/$(basename "$output")"
  for path in "$app" "$config" "$genesis" "$binary"; do
    [[ "$output_path" != "$path" ]] || die "output: must not overwrite input $path"
    if [[ -e "$output_path" && "$output_path" -ef "$path" ]]; then
      die "output: hardlink alias of input $path"
    fi
  done
  scratch_parent=$output_parent
else
  scratch_parent=${TMPDIR:-$PWD}
  scratch_parent=$(cd -P "$scratch_parent" 2>/dev/null && pwd -P) \
    || die "scratch: cannot resolve TMPDIR or current directory"
fi

umask 077
scratch=$(mktemp -d "$scratch_parent/.validator-mempool-preflight.XXXXXX") \
  || die "scratch: cannot create private directory under $scratch_parent"

hash_file() {
  local digest
  digest=$("${hash_command[@]}" <"$1" | awk 'NR == 1 {print $1}') || return 1
  [[ "$digest" =~ ^[0-9a-fA-F]{64}$ ]] || return 1
  printf '%s\n' "$digest"
}
app_hash=$(hash_file "$app") || die "app.toml: cannot calculate SHA-256"
config_hash=$(hash_file "$config") || die "config.toml: cannot calculate SHA-256"
genesis_hash=$(hash_file "$genesis") || die "genesis.json: cannot calculate SHA-256"
binary_hash=$(hash_file "$binary") || die "node binary: cannot calculate SHA-256"

snapshot="$scratch/snapshot"
snapshot_binary_dir="$snapshot/bin"
mkdir -p "$snapshot_binary_dir" || die "snapshot: cannot create private directory"
snapshot_app="$snapshot/app.toml"
snapshot_config="$snapshot/config.toml"
snapshot_genesis="$snapshot/genesis.json"
snapshot_binary="$snapshot_binary_dir/gurud"
command cp "$app" "$snapshot_app" || die "app.toml: cannot create private snapshot"
command cp "$config" "$snapshot_config" || die "config.toml: cannot create private snapshot"
command cp "$genesis" "$snapshot_genesis" || die "genesis.json: cannot create private snapshot"
command cp "$binary" "$snapshot_binary" || die "node binary: cannot create private snapshot"
chmod 700 "$snapshot_binary" || die "node binary: cannot secure private snapshot"
[[ "$(hash_file "$snapshot_app")" == "$app_hash" ]] \
  || die "app.toml: file changed while creating snapshot"
[[ "$(hash_file "$snapshot_config")" == "$config_hash" ]] \
  || die "config.toml: file changed while creating snapshot"
[[ "$(hash_file "$snapshot_genesis")" == "$genesis_hash" ]] \
  || die "genesis.json: file changed while creating snapshot"
[[ "$(hash_file "$snapshot_binary")" == "$binary_hash" ]] \
  || die "node binary: file changed while creating snapshot"

run_json() {
  local label=$1 destination=$2 status detail
  shift 2
  if env -i HOME="$parser_home" PATH="${PATH:-/usr/bin:/bin}" TMPDIR="$scratch" \
    "$snapshot_binary" "$@" >"$destination" 2>"$scratch/stderr"; then
    :
  else
    status=$?
    detail=$(tr '\n' ' ' <"$scratch/stderr")
    [[ -n "$detail" ]] || detail="exit status $status"
    die "$label: gurud command failed: $detail"
  fi
  jq -s -e 'length == 1 and (.[0] | type == "object")' \
    "$destination" >/dev/null 2>&1 \
    || die "$label: gurud output must be exactly one JSON object"
}

parser_home="$scratch/parser-home"
mkdir -p "$parser_home/config" || die "parser: cannot create private home"
ln -s "$snapshot_app" "$parser_home/config/app.toml" \
  || die "app.toml: cannot create runtime parser input"
ln -s "$snapshot_config" "$parser_home/config/config.toml" \
  || die "config.toml: cannot create runtime parser input"
run_json "node version" "$scratch/version.json" \
  --home "$parser_home" version --long --output json
ln -s "$snapshot_app" "$parser_home/config/rendered-app.toml" \
  || die "app.toml: cannot create parser input"
ln -s "$snapshot_config" "$parser_home/config/rendered-config.toml" \
  || die "config.toml: cannot create parser input"
run_json "app.toml" "$scratch/app.json" \
  --home "$parser_home" config view rendered-app --output-format json
run_json "config.toml" "$scratch/config.json" \
  --home "$parser_home" config view rendered-config --output-format json

jq -e '
  (if (.server_name | type) == "string" then (.server_name | test("\\S")) else false end) and
  ((.version | type) == "string") and ((.commit | type) == "string")
' \
  "$scratch/version.json" >/dev/null 2>&1 \
  || die "node version: server_name must be non-empty; version and commit must be strings"
jq -e 'if type == "object" then
  (.chain_id | if type == "string" then test("\\S") else false end)
  else false end' \
  "$snapshot_genesis" >/dev/null 2>&1 \
  || die "genesis.json: chain_id must be a non-empty string"
jq '{chain_id}' "$snapshot_genesis" >"$scratch/network.json" \
  || die "genesis.json: cannot record chain_id"

checked_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ') || die "cannot determine UTC time"

jq -n \
  --arg schema "$schema" --arg checked_at "$checked_at" \
  --arg validator_id "$validator_id" --arg home "$home" \
  --arg binary "$binary" --arg binary_hash "$binary_hash" \
  --arg app_path "$app" --arg app_hash "$app_hash" \
  --arg config_path "$config" --arg config_hash "$config_hash" \
  --arg genesis_path "$genesis" --arg genesis_hash "$genesis_hash" \
  --slurpfile app "$scratch/app.json" --slurpfile config "$scratch/config.json" \
  --slurpfile version "$scratch/version.json" --slurpfile network "$scratch/network.json" '
    def field($doc; $key):
      if (($doc.mempool? | type) == "object") then
        if ($doc.mempool | has($key))
        then {present: true, value: $doc.mempool[$key]}
        else {present: false, value: "<missing>"}
        end
      else {present: false, value: "<missing>"}
      end;
    def typename($f): if $f.present then ($f.value | type) else "missing" end;
    def check($key; $class; $f; $expected; $ok): {
      key: $key, classification: $class, actual: $f.value, expected: $expected,
      status: (if $ok then "pass" else "fail" end), actual_type: typename($f)
    };
    def boolok($f): $f.present and (($f.value | type) == "boolean");
    def intok($f):
      if $f.present and (($f.value | type) == "number")
      then ($f.value >= 0) and (($f.value | floor) == $f.value)
      else false end;
    def durationok($f):
      if $f.present and (($f.value | type) == "string")
      then ($f.value | test("^[+-]?(?:0|(?:(?:[0-9]+(?:\\.[0-9]*)?|\\.[0-9]+)(?:ns|us|µs|μs|ms|s|m|h))+)$"))
      else false end;

    $app[0] as $a | $config[0] as $c | $version[0] as $v |
    field($a; "max-txs") as $max | field($c; "type") as $type |
    field($c; "recheck") as $recheck | field($c; "broadcast") as $broadcast |
    field($c; "keep-invalid-txs-in-cache") as $invalid_cache |
    field($c; "wal_dir") as $wal_dir |
    field($c; "size") as $size | field($c; "max_txs_bytes") as $max_bytes |
    field($c; "cache_size") as $cache | field($c; "max_tx_bytes") as $max_tx |
    field($c; "max_batch_bytes") as $max_batch |
    field($c; "recheck_timeout") as $timeout |
    field($c; "experimental_max_gossip_connections_to_persistent_peers") as $persistent_gossip |
    field($c; "experimental_max_gossip_connections_to_non_persistent_peers") as $non_persistent_gossip |
    [
      check("app.toml:mempool.max-txs"; "required"; $max; -1; $max.present and $max.value == -1),
      check("config.toml:mempool.type"; "required"; $type; "flood"; $type.present and $type.value == "flood"),
      check("config.toml:mempool.recheck"; "structural"; $recheck; "boolean"; boolok($recheck)),
      check("config.toml:mempool.broadcast"; "structural"; $broadcast; "boolean"; boolok($broadcast)),
      check("config.toml:mempool.keep-invalid-txs-in-cache"; "structural"; $invalid_cache; "boolean"; boolok($invalid_cache)),
      check("config.toml:mempool.wal_dir"; "structural"; $wal_dir; "string"; $wal_dir.present and (($wal_dir.value | type) == "string")),
      check("config.toml:mempool.size"; "structural"; $size; "non-negative integer"; intok($size)),
      check("config.toml:mempool.max_txs_bytes"; "structural"; $max_bytes; "non-negative integer"; intok($max_bytes)),
      check("config.toml:mempool.cache_size"; "structural"; $cache; "non-negative integer"; intok($cache)),
      check("config.toml:mempool.max_tx_bytes"; "structural"; $max_tx; "non-negative integer"; intok($max_tx)),
      check("config.toml:mempool.max_batch_bytes"; "structural"; $max_batch; "non-negative integer"; intok($max_batch)),
      check("config.toml:mempool.recheck_timeout"; "structural"; $timeout; "Go duration string"; durationok($timeout)),
      check("config.toml:mempool.experimental_max_gossip_connections_to_persistent_peers"; "structural"; $persistent_gossip; "non-negative integer"; intok($persistent_gossip)),
      check("config.toml:mempool.experimental_max_gossip_connections_to_non_persistent_peers"; "structural"; $non_persistent_gossip; "non-negative integer"; intok($non_persistent_gossip)),
      check("node.server_name"; "required"; {present: true, value: $v.server_name}; "gurud"; $v.server_name == "gurud")
    ] as $checks |
    (if boolok($recheck) and ($recheck.value == false) then [{
      code: "MEMPOOL_RECHECK_DISABLED", severity: "warning",
      key: "config.toml:mempool.recheck", actual: false, recommended: true
    }] else [] end) as $warnings |
    (if any($checks[]; .status == "fail") then "fail"
     elif ($warnings | length) > 0 then "pass_with_warnings" else "pass" end) as $status |
    {
      schema: $schema, status: $status, checked_at: $checked_at,
      validator_id: $validator_id, home: $home,
      scope: {kind: "rendered-config-files", runtime_overrides_checked: false,
        note: "Evidence covers the rendered files and binary hashes below; CLI and service-environment overrides are outside its scope."},
      node: {path: $binary, sha256: $binary_hash, server_name: $v.server_name,
        version: $v.version, commit: $v.commit, version_info: $v},
      network: $network[0],
      files: {
        "app.toml": {path: $app_path, sha256: $app_hash},
        "config.toml": {path: $config_path, sha256: $config_hash},
        "genesis.json": {path: $genesis_path, sha256: $genesis_hash}
      },
      checks: $checks, warnings: $warnings
    }
  ' >"$scratch/evidence.json" || die "cannot construct JSON evidence"

unchanged() {
  local current
  current=$(hash_file "$2") || die "$1: cannot recalculate SHA-256"
  [[ "$current" == "$3" ]] || die "$1: file changed during verification"
}
unchanged "app.toml" "$app" "$app_hash"
unchanged "config.toml" "$config" "$config_hash"
unchanged "genesis.json" "$genesis" "$genesis_hash"
unchanged "node binary" "$binary" "$binary_hash"

status=$(jq -r '.status' "$scratch/evidence.json") || die "cannot read evidence status"
jq -r '.checks[] | select(.status == "fail") |
  (if .classification == "required" then "REQUIRED" else "STRUCTURAL" end) +
  " " + .key + ": got " + (.actual | tojson) + "; want " + (.expected | tojson)' \
  "$scratch/evidence.json" >&2
jq -r '.warnings[] | "WARNING " + .key + ": got " + (.actual | tojson) +
  "; recommend " + (.recommended | tojson) + "; code=" + .code' \
  "$scratch/evidence.json" >&2

evidence="$scratch/evidence.json"
if [[ -n "$output_path" ]]; then
  [[ ! -L "$output_path" ]] || die "output: symlink targets are not allowed: $output_path"
  [[ ! -e "$output_path" || -f "$output_path" ]] \
    || die "output: expected regular file target: $output_path"
  for path in "$app" "$config" "$genesis" "$binary"; do
    [[ "$output_path" != "$path" ]] || die "output: must not overwrite input $path"
    if [[ -e "$output_path" && "$output_path" -ef "$path" ]]; then
      die "output: hardlink alias of input $path"
    fi
  done
  chmod 600 "$evidence" || die "output: cannot set private permissions"
  mv -f "$evidence" "$output_path" || die "output: cannot atomically publish $output_path"
  evidence=$output_path
else
  command cat "$evidence" || die "cannot write evidence to stdout"
fi
[[ "$status" != "fail" ]] || exit "$exit_policy"
