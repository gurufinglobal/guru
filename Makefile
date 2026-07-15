#!/usr/bin/make -f

###############################################################################
###                           Module & Versioning                           ###
###############################################################################

APP_NAME := guru

VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT := $(shell git log -1 --format='%H' 2>/dev/null || echo "unknown")
TMVERSION := $(shell go list -m github.com/cometbft/cometbft | sed 's:.* ::')

###############################################################################
###                          Directories & Binaries                         ###
###############################################################################

BUILDDIR ?= $(CURDIR)/build
CMD_MAIN_FILES := $(wildcard cmd/*/main.go)
CMD_BINS := $(notdir $(patsubst %/,%,$(dir $(CMD_MAIN_FILES))))
CMD_BUILD_TARGETS := $(addprefix $(BUILDDIR)/,$(CMD_BINS))

export GO111MODULE = on
export CGO_ENABLED ?= 1

###############################################################################
###                              Build Flags                                ###
###############################################################################

build_tags = netgo
ifeq ($(cleveldb),1)
  build_tags += gcc
  build_tags += cleveldb
endif
build_tags := $(strip $(build_tags))

whitespace := $(subst ,, )
comma := ,
build_tags_comma_sep := $(subst $(whitespace),$(comma),$(build_tags))

ldflags_common = -X github.com/cosmos/cosmos-sdk/version.Name=$(APP_NAME) \
                 -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
                 -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
                 -X github.com/cometbft/cometbft/version.TMCoreSemVer=$(TMVERSION) \
                 -X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)"

ifeq (,$(findstring nostrip,$(COSMOS_BUILD_OPTIONS)))
  ldflags_common += -w -s
endif
ldflags_common += $(LDFLAGS)
ldflags = $(strip $(ldflags_common) -X github.com/cosmos/cosmos-sdk/version.AppName=$(1))

BUILD_FLAGS := -mod=readonly -tags "$(build_tags)" -trimpath

###############################################################################
###                                Commands                                 ###
###############################################################################

.PHONY: all build install clean format lint test FORCE

all: build

build: $(CMD_BUILD_TARGETS)
	@echo "Built $(CMD_BINS) to $(BUILDDIR)."

$(BUILDDIR)/%: go.mod go.sum FORCE
	@echo "Building $* to $(BUILDDIR) ..."
	@mkdir -p $(BUILDDIR)
	@go build $(BUILD_FLAGS) -ldflags '$(call ldflags,$*)' -o $@ ./cmd/$*

install: go.mod go.sum
	@echo "Installing gurud to $(GOPATH)/bin ..."
	@go install $(BUILD_FLAGS) -ldflags '$(call ldflags,gurud)' ./cmd/gurud

clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILDDIR)

FORCE:

###############################################################################
###                           Testing & Linting                             ###
###############################################################################

test:
	@echo "Running unit tests..."
	@go test -mod=readonly -race -cover ./...
	@echo "Running two-chain TransSwap proof-relay acceptance test..."
	@go test -mod=readonly -tags=test -race -cover ./tests/transwaptwochain

format:
	@echo "Formatting Go files..."
	@find . -name '*.go' -type f -not -path "*/vendor*" -not -name '*.pb.go' -not -name '*.pb.gw.go' -not -name '*.pulsar.go' | xargs gofumpt -w -l

lint:
	@echo "Running golangci-lint..."
	@golangci-lint run --timeout=15m

###############################################################################
###                                Protobuf                                 ###
###############################################################################

BUF_IMAGE := ghcr.io/cosmos/proto-builder:0.18.1
DOCKER_BUF := docker run --rm -v $(CURDIR):/workspace --workdir /workspace --user 0 $(BUF_IMAGE) buf

.PHONY: proto-all proto-gen proto-format proto-lint

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Downloading Protobuf dependencies (buf dep update)..."
	@$(DOCKER_BUF) dep update proto
	@echo "Generating Protobuf files (*.pulsar.go, *_grpc.pb.go, *.pb.gw.go)..."
	@$(DOCKER_BUF) generate proto --template proto/buf.gen.pulsar.yaml
	@echo "Protobuf generation complete."

proto-format:
	@echo "Formatting Protobuf files..."
	@$(DOCKER_BUF) format -w proto

proto-lint:
	@echo "Linting Protobuf files..."
	@$(DOCKER_BUF) lint proto
