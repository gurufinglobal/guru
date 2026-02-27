#!/usr/bin/env bash

# --------------
# Commands to run locally
# docker run --network host --rm -v $(CURDIR):/workspace --workdir /workspace ghcr.io/cosmos/proto-builder:v0.11.6 sh ./generate_protos.sh
#
set -eo pipefail

proto_dirs=$(find ./proto -path -prune -o -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
for dir in $proto_dirs; do
	proto_files=$(find "${dir}" -maxdepth 1 -name '*.proto')
	for file in $proto_files; do
		# Check if the go_package in the file is pointing to guru
		if grep -q "option go_package.*guru" "$file"; then
			buf generate --template proto/buf.gen.gogo.yaml "$file"
		fi
	done
done

# move proto files to the right places
#
# NOTE: buf generate output is copied into the repo root. Historically, this step
# has accidentally overwritten go.mod/go.sum in some environments. Keep a backup
# and restore them to avoid breaking the module graph.
cp -f go.mod /tmp/go.mod.bak
cp -f go.sum /tmp/go.sum.bak
cp -r github.com/gurufinglobal/guru/v2/* ./
mv -f /tmp/go.mod.bak go.mod
mv -f /tmp/go.sum.bak go.sum
rm -rf github.com
