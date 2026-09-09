#!/usr/bin/env bash

set -euo pipefail

readonly default_image='ghcr.io/goreleaser/goreleaser-cross:v1.26.5-v2.17.1@sha256:0cf2b7f757b40397d2bef5423adb88d0ac63899e88a9f0c4bbb370d3fb7b2fb5'
readonly image="${GORELEASER_CROSS_IMAGE:-$default_image}"
readonly repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly container_workdir='/workspace'
readonly module_go_version="$(awk '$1 == "go" { print $2; exit }' "$repository_root/go.mod")"
readonly oracle_go_version="$(awk '$1 == "go" { print $2; exit }' "$repository_root/oracle/go.mod")"
readonly host_temp_root="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
readonly container_cache_root='/goreleaser-cache'

if [[ ! "$module_go_version" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
  echo >&2 "unable to determine Go toolchain version from $repository_root/go.mod"
  exit 2
fi

if [[ ! "$oracle_go_version" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
  echo >&2 "unable to determine Go toolchain version from $repository_root/oracle/go.mod"
  exit 2
fi

if [[ "$module_go_version" != "$oracle_go_version" ]]; then
  echo "Go version mismatch: go.mod=$module_go_version oracle/go.mod=$oracle_go_version" >&2
  exit 2
fi

readonly release_toolchain="go${module_go_version}"
readonly default_cache_root="${host_temp_root%/}/goreleaser-cross/$release_toolchain"
readonly cache_root="${GORELEASER_CROSS_CACHE_DIR:-$default_cache_root}"
readonly release_mode="${1:-}"

usage() {
  echo "usage: $0 {check|snapshot|release}" >&2
  exit 2
}

case "${1:-}" in
  check)
    goreleaser_args=(check)
    ;;
  snapshot)
    goreleaser_args=(release --snapshot --clean --skip=publish --parallelism=1)
    ;;
  release)
    if [[ -z "${GITHUB_TOKEN:-}" ]]; then
      echo 'GITHUB_TOKEN is required for release publishing' >&2
      exit 2
    fi
    goreleaser_args=(release --clean --parallelism=1)
    ;;
  *)
    usage
    ;;
esac

tm_version="${TMVERSION:-$(go list -m -f '{{ .Version }}' github.com/cometbft/cometbft)}"
mkdir -p "$cache_root"

docker_args=(
  run
  --rm
  --user "$(id -u):$(id -g)"
  --env "HOME=$container_cache_root/home"
  --env "GOCACHE=$container_cache_root/go-build"
  --env "GOMODCACHE=$container_cache_root/go-mod"
  --env "GOTMPDIR=$container_cache_root/go-tmp"
  --env "TMPDIR=$container_cache_root/tmp"
  --env "GOTOOLCHAIN=$release_toolchain"
  --env "TMVERSION=$tm_version"
  --env "GORELEASER_CROSS_MODE=$release_mode"
  --volume "$repository_root:$container_workdir"
  --volume "$cache_root:$container_cache_root"
  --workdir "$container_workdir"
  --entrypoint /bin/bash
)

if [[ "${1:-}" == 'release' ]]; then
  docker_args+=(--env GITHUB_TOKEN)
fi

docker "${docker_args[@]}" "$image" -euc '
  mkdir -p "$HOME" "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR" "$TMPDIR"
  git config --global --add safe.directory "$PWD"

  toolchain_os="$(GOTOOLCHAIN=local go env GOHOSTOS)"
  toolchain_arch="$(GOTOOLCHAIN=local go env GOHOSTARCH)"
  toolchain_cache="$GOMODCACHE/golang.org/toolchain@v0.0.1-${GOTOOLCHAIN}.${toolchain_os}-${toolchain_arch}"

  case "$toolchain_cache" in
    "$GOMODCACHE"/golang.org/toolchain@v0.0.1-go*.*-*) ;;
    *)
      echo >&2 "refusing to repair unexpected Go toolchain cache path: $toolchain_cache"
      exit 2
      ;;
  esac

  if [[ -d "$toolchain_cache" && ! -x "$toolchain_cache/bin/go" ]]; then
    echo >&2 "removing incomplete Go toolchain cache: $toolchain_cache"
    chmod -R u+w -- "$toolchain_cache"
    rm -rf -- "$toolchain_cache"
  fi

  go version

  if [[ "$GORELEASER_CROSS_MODE" != "check" ]]; then
    # Cross-target cache entries are not useful enough to justify carrying a
    # partially written build cache into a release build.
    GOTOOLCHAIN=local go clean -cache

    if ! go mod verify >/dev/null 2>&1 ||
      ! (cd oracle && GOWORK=off go mod verify >/dev/null 2>&1); then
      echo >&2 "Go module cache integrity check failed; rebuilding the module cache"
      GOTOOLCHAIN=local go clean -modcache
      go version
    fi
  fi

  exec goreleaser "$@"
' -- "${goreleaser_args[@]}"
