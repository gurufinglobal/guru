#!/usr/bin/env bash

set -euo pipefail

readonly default_image='ghcr.io/goreleaser/goreleaser-cross:v1.26.5-v2.17.1@sha256:0cf2b7f757b40397d2bef5423adb88d0ac63899e88a9f0c4bbb370d3fb7b2fb5'
readonly image="${GORELEASER_CROSS_IMAGE:-$default_image}"
readonly repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly container_workdir='/workspace'
readonly host_temp_root="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
readonly cache_root="${GORELEASER_CROSS_CACHE_DIR:-${host_temp_root%/}/goreleaser-cross}"
readonly container_cache_root='/goreleaser-cache'

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
  --env GOTOOLCHAIN=go1.23.8
  --env "TMVERSION=$tm_version"
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
  exec goreleaser "$@"
' -- "${goreleaser_args[@]}"
