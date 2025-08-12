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
		# Check if the go_package in the file is pointing to evmos
		if grep -q "option go_package.*guru-v2" "$file"; then
			buf generate --template proto/buf.gen.gogo.yaml "$file"
		fi
	done
done

# move proto files to the right places
cp -r github.com/GPTx-global/guru-v2/* ./
rm -rf github.com
