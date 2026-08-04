#!/usr/bin/make -f

###############################################################################
###                           Module & Versioning                           ###
###############################################################################

APP_NAME := guru

VERSION := $(shell git describe --tags --match 'v[0-9]*' --always 2>/dev/null || echo "dev")
ORACLE_VERSION := $(shell value=$$(git describe --tags --match 'oracle/v[0-9]*' --always 2>/dev/null); if test -n "$$value"; then printf '%s\n' "$$value" | sed 's|^oracle/||'; else echo "dev"; fi)
COMMIT := $(shell git log -1 --format='%H' 2>/dev/null || echo "unknown")
TMVERSION := $(shell go list -m github.com/cometbft/cometbft | sed 's:.* ::')

###############################################################################
###                          Directories & Binaries                         ###
###############################################################################

BUILDDIR ?= $(CURDIR)/build
CMD_MAIN_FILES := $(wildcard cmd/*/main.go)
CMD_BINS := $(notdir $(patsubst %/,%,$(dir $(CMD_MAIN_FILES))))
CMD_BUILD_TARGETS := $(addprefix $(BUILDDIR)/,$(CMD_BINS))
ORACLE_BUILD_TARGET := $(BUILDDIR)/oracled

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

build: $(CMD_BUILD_TARGETS) $(ORACLE_BUILD_TARGET)
	@echo "Built $(CMD_BINS) and oracled to $(BUILDDIR)."

$(BUILDDIR)/%: go.mod go.sum FORCE
	@echo "Building $* to $(BUILDDIR) ..."
	@mkdir -p $(BUILDDIR)
	@go build $(BUILD_FLAGS) -ldflags '$(call ldflags,$*)' -o $@ ./cmd/$*

$(ORACLE_BUILD_TARGET): oracle/go.mod oracle/go.sum FORCE
	@echo "Building oracled to $(BUILDDIR) ..."
	@mkdir -p $(BUILDDIR)
	@CGO_ENABLED=0 GOWORK=off go -C oracle build -mod=readonly -trimpath \
		-ldflags '-w -s -X github.com/gurufinglobal/guru/oracle/internal/version.Version=$(ORACLE_VERSION) -X github.com/gurufinglobal/guru/oracle/internal/version.Commit=$(COMMIT)' \
		-o $(abspath $@) ./cmd/oracled

install: go.mod go.sum oracle/go.mod oracle/go.sum
	@echo "Installing gurud and oracled to $(GOPATH)/bin ..."
	@go install $(BUILD_FLAGS) -ldflags '$(call ldflags,gurud)' ./cmd/gurud
	@CGO_ENABLED=0 GOWORK=off go -C oracle install -mod=readonly -trimpath \
		-ldflags '-w -s -X github.com/gurufinglobal/guru/oracle/internal/version.Version=$(ORACLE_VERSION) -X github.com/gurufinglobal/guru/oracle/internal/version.Commit=$(COMMIT)' \
		./cmd/oracled

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
	@GOWORK=off go -C oracle test -mod=readonly -race -cover ./...
	@echo "Running two-chain TransSwap proof-relay acceptance test..."
	@go test -mod=readonly -tags=test -race -cover ./tests/transwaptwochain

format:
	@echo "Formatting Go files..."
	@find . -name '*.go' -type f -not -path "*/vendor*" -not -name '*.pb.go' -not -name '*.pb.gw.go' -not -name '*.pulsar.go' | xargs gofumpt -w -l

lint:
	@echo "Running golangci-lint..."
	@golangci-lint run ./... --timeout=15m
	@cd oracle && GOWORK=off golangci-lint run ./... --timeout=15m

###############################################################################
###                                Protobuf                                 ###
###############################################################################

BUF_IMAGE := ghcr.io/cosmos/proto-builder:0.18.1
DOCKER_PROTO := docker run --rm -v $(CURDIR):/workspace --workdir /workspace --user 0 $(BUF_IMAGE)
DOCKER_BUF := $(DOCKER_PROTO) buf

.PHONY: proto-all proto-gen proto-format proto-lint

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Downloading Protobuf dependencies (buf dep update)..."
	@$(DOCKER_BUF) dep update proto
	@echo "Generating internal gogo and external Pulsar Protobuf files..."
	@$(DOCKER_PROTO) sh scripts/proto-gen.sh
	@echo "Protobuf generation complete."

proto-format:
	@echo "Formatting Protobuf files..."
	@$(DOCKER_BUF) format -w proto

proto-lint:
	@echo "Linting Protobuf files..."
	@$(DOCKER_BUF) lint proto
