#!/usr/bin/env sh

set -eu

stage=.proto-gen
gogo_stage="$stage/gogo"
gogo_module_root="$gogo_stage/github.com/gurufinglobal/guru/v2"
gogo_generated_root="$gogo_module_root/x"

cleanup() {
  rm -rf "$stage"
}

trap cleanup EXIT

rm -rf "$stage"
buf generate proto --template proto/buf.gen.gogo.yaml

unexpected="$stage/unexpected-gogo-paths"
find "$gogo_module_root" -type f \
  \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -print |
  while IFS= read -r file; do
    relative=${file#"$gogo_module_root/"}
    directory=${relative%/*}
    case "$directory" in
      x/*/types) ;;
      *) printf '%s\n' "$relative" >> "$unexpected" ;;
    esac
  done

if [ -s "$unexpected" ]; then
  echo "generated files must use github.com/gurufinglobal/guru/v2/x/<module>/types:" >&2
  sort "$unexpected" >&2
  exit 1
fi

find x -type d -name types -print |
  while IFS= read -r destination; do
    if [ -d "$gogo_module_root/$destination" ]; then
      continue
    fi
    find "$destination" -maxdepth 1 -type f \
      \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -delete
  done

generated_dirs="$stage/generated-gogo-dirs"
find "$gogo_generated_root" -type d -name types -print > "$generated_dirs"
if [ ! -s "$generated_dirs" ]; then
  echo "no generated gogo packages found under $gogo_generated_root" >&2
  exit 1
fi

while IFS= read -r generated; do
  destination=${generated#"$gogo_module_root/"}
  case "$destination" in
    x/*/types) ;;
    *)
      echo "invalid generated package directory: $destination" >&2
      exit 1
      ;;
  esac

  mkdir -p "$destination"
  find "$destination" -maxdepth 1 -type f \
    \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -delete
  cp -R "$generated/." "$destination/"
done < "$generated_dirs"
