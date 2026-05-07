#!/usr/bin/make -f

###############################################################################
###                           Module & Versioning                           ###
###############################################################################

APP_NAME := guru
BINARY_NAME := gurud

VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT := $(shell git log -1 --format='%H' 2>/dev/null || echo "unknown")
TMVERSION := $(shell go list -m github.com/cometbft/cometbft | sed 's:.* ::')

###############################################################################
###                          Directories & Binaries                         ###
###############################################################################

BUILDDIR ?= $(CURDIR)/build
MAIN_PKG := ./cmd/gurud

export GO111MODULE = on

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

ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=$(APP_NAME) \
          -X github.com/cosmos/cosmos-sdk/version.AppName=$(BINARY_NAME) \
          -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
          -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
          -X github.com/cometbft/cometbft/version.TMCoreSemVer=$(TMVERSION) \
          -X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)"

ifeq (,$(findstring nostrip,$(COSMOS_BUILD_OPTIONS)))
  ldflags += -w -s
endif
ldflags += $(LDFLAGS)
ldflags := $(strip $(ldflags))

BUILD_FLAGS := -tags "$(build_tags)" -ldflags '$(ldflags)' -trimpath

###############################################################################
###                                Commands                                 ###
###############################################################################

.PHONY: all build install clean format lint test

all: build

build: go.sum
	@echo "Building $(BINARY_NAME) to $(BUILDDIR) ..."
	@mkdir -p $(BUILDDIR)
	@go build $(BUILD_FLAGS) -o $(BUILDDIR)/$(BINARY_NAME) $(MAIN_PKG)

install: go.sum
	@echo "Installing $(BINARY_NAME) to $(GOPATH)/bin ..."
	@go install $(BUILD_FLAGS) $(MAIN_PKG)

clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILDDIR)

go.sum: go.mod
	@echo "Ensuring dependencies are completely synced..."
	@go mod tidy

###############################################################################
###                           Testing & Linting                             ###
###############################################################################

test:
	@echo "Running unit tests..."
	@go test -mod=readonly -race -cover ./...

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
	@echo "Generating Gogo Protobuf files (*.pb.go) for State Machine..."
	@$(DOCKER_BUF) generate proto --template proto/buf.gen.gogo.yaml
	@echo "Generating Pulsar Protobuf files (*.pulsar.go) for SDK v0.50+ API..."
	@$(DOCKER_BUF) generate proto --template proto/buf.gen.pulsar.yaml
	@echo "Relocating Gogo files to internal modules..."
	@cp -r github.com/gurufinglobal/guru/v3/* ./
	@rm -rf github.com
	@echo "Protobuf generation complete."

proto-format:
	@echo "Formatting Protobuf files..."
	@$(DOCKER_BUF) format -w proto

proto-lint:
	@echo "Linting Protobuf files..."
	@$(DOCKER_BUF) lint proto