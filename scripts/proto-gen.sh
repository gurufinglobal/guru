#!/usr/bin/env sh

set -eu

stage=.proto-gen
gogo_stage="$stage/gogo"
gogo_module_root="$gogo_stage/github.com/gurufinglobal/guru/v3"
gogo_generated_root="$gogo_module_root/x"
pulsar_stage="$stage/pulsar/api/guru"
pulsar_destination=api/guru
previous="$stage/previous-api-guru"

cleanup() {
  if [ -d "$previous" ] && [ ! -d "$pulsar_destination" ]; then
    mv "$previous" "$pulsar_destination"
  fi
  rm -rf "$stage"
}

trap cleanup EXIT

rm -rf "$stage"
buf generate proto --template proto/buf.gen.gogo.yaml
buf generate proto --template proto/buf.gen.pulsar.yaml --output "$stage/pulsar"

unexpected="$stage/unexpected-gogo-paths"
find "$gogo_module_root" -type f \
  \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -print |
  while IFS= read -r file; do
    relative=${file#"$gogo_module_root/"}
    case "$relative" in
      x/*/types/*)
        module=${relative#x/}
        module=${module%%/types/*}
        case "$module" in
          "" | */*) printf '%s\n' "$relative" >> "$unexpected" ;;
        esac
        ;;
      *) printf '%s\n' "$relative" >> "$unexpected" ;;
    esac
  done

if [ -s "$unexpected" ]; then
  echo "generated gogo files must use github.com/gurufinglobal/guru/v3/x/<module>/types:" >&2
  sort "$unexpected" >&2
  exit 1
fi

if [ ! -d "$pulsar_stage" ]; then
  echo "missing generated Pulsar API root: $pulsar_stage" >&2
  exit 1
fi

for destination in x/*/types; do
  if [ ! -d "$destination" ] || [ -d "$gogo_module_root/$destination" ]; then
    continue
  fi
  find "$destination" -maxdepth 1 -type f \
    \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -delete
done

found=false
for generated in "$gogo_generated_root"/*/types; do
  if [ ! -d "$generated" ]; then
    continue
  fi

  found=true
  destination=${generated#"$gogo_module_root/"}
  mkdir -p "$destination"

  find "$destination" -maxdepth 1 -type f \
    \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -delete
  cp -R "$generated/." "$destination/"
done

if [ "$found" = false ]; then
  echo "no generated gogo packages found under $gogo_generated_root/*/types" >&2
  exit 1
fi

mkdir -p api
if [ -d "$pulsar_destination" ]; then
  mv "$pulsar_destination" "$previous"
fi
if ! mv "$pulsar_stage" "$pulsar_destination"; then
  exit 1
fi
rm -rf "$previous"
