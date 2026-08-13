#!/usr/bin/make -f

.DEFAULT_GOAL := build

###############################################################################
###                           Module & Versioning                           ###
###############################################################################

GO ?= go
VERSION ?= $(shell (git describe --tags --always 2>/dev/null || echo dev) | sed 's/^v//')
ORACLE_VERSION ?= $(shell value=$$(git describe --tags --match 'oracle/v[0-9]*' --always 2>/dev/null); if test -n "$$value"; then printf '%s\n' "$$value" | sed 's|^oracle/||'; else echo dev; fi)
COMMIT ?= $(shell git log -1 --format='%H' 2>/dev/null || echo unknown)
TMVERSION := $(shell $(GO) list -m github.com/cometbft/cometbft 2>/dev/null | sed 's:.* ::')

###############################################################################
###                          Directories & Binaries                         ###
###############################################################################

BINARY ?= gurud
ORACLE_BINARY ?= oracled
BINDIR ?= $(shell $(GO) env GOPATH)/bin
BUILDDIR ?= $(CURDIR)/build
# Keep BUILD_DIR as a compatibility override for the original Guru Makefile.
BUILD_DIR ?= $(BUILDDIR)
MAIN_PKG := ./cmd/gurud
ORACLE_MAIN_PKG := ./cmd/oracled

export GO111MODULE = on

###############################################################################
###                            Build & Install                              ###
###############################################################################

COSMOS_BUILD_OPTIONS ?=
BUILD_TAGS ?=
LDFLAGS ?=
CGO_ENABLED ?= 1

build_tags = netgo

ifeq (cleveldb,$(findstring cleveldb,$(COSMOS_BUILD_OPTIONS)))
  build_tags += gcc
endif

build_tags += $(BUILD_TAGS)
build_tags := $(strip $(build_tags))

ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=guru \
          -X github.com/cosmos/cosmos-sdk/version.AppName=$(BINARY) \
          -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
          -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
          -X github.com/cometbft/cometbft/version.TMCoreSemVer=$(TMVERSION)

ifeq (cleveldb,$(findstring cleveldb,$(COSMOS_BUILD_OPTIONS)))
  ldflags += -X github.com/cosmos/cosmos-sdk/types.DBBackend=cleveldb
endif

whitespace := $(subst ,, )
comma := ,
build_tags_comma_sep := $(subst $(whitespace),$(comma),$(build_tags))
ldflags += -X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)"

ifeq (,$(findstring nostrip,$(COSMOS_BUILD_OPTIONS)))
  ldflags += -w -s
endif

ldflags += $(LDFLAGS)
ldflags := $(strip $(ldflags))

ifeq (staticlink,$(findstring staticlink,$(COSMOS_BUILD_OPTIONS)))
  ldflags += -linkmode external -extldflags '-static'
endif

BUILD_FLAGS := -mod=readonly -tags "$(build_tags)" -ldflags '$(ldflags)'

ifeq (,$(findstring nostrip,$(COSMOS_BUILD_OPTIONS)))
  BUILD_FLAGS += -trimpath
endif

ifneq (,$(findstring nooptimization,$(COSMOS_BUILD_OPTIONS)))
  BUILD_FLAGS += -gcflags "all=-N -l"
endif

all: build

build: build-gurud build-oracled

build-gurud: | $(BUILD_DIR)/
	@echo "Building $(BINARY) to $(BUILD_DIR)/$(BINARY)"
	@CGO_ENABLED="$(CGO_ENABLED)" $(GO) build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(BINARY) $(MAIN_PKG)

build-oracled: | $(BUILD_DIR)/
	@echo "Building $(ORACLE_BINARY) to $(BUILD_DIR)/$(ORACLE_BINARY)"
	@CGO_ENABLED=0 GOWORK=off $(GO) -C oracle build -mod=readonly -trimpath \
		-ldflags '-w -s -X github.com/gurufinglobal/guru/oracle/internal/version.Version=$(ORACLE_VERSION) -X github.com/gurufinglobal/guru/oracle/internal/version.Commit=$(COMMIT)' \
		-o $(abspath $(BUILD_DIR)/$(ORACLE_BINARY)) $(ORACLE_MAIN_PKG)

build-linux:
	@GOOS=linux GOARCH=amd64 $(MAKE) build

install:
	@echo "Installing $(BINARY) to $(BINDIR)"
	@CGO_ENABLED="$(CGO_ENABLED)" GOBIN="$(BINDIR)" \
		$(GO) install $(BUILD_FLAGS) $(MAIN_PKG)
	@echo "Installing $(ORACLE_BINARY) to $(BINDIR)"
	@CGO_ENABLED=0 GOWORK=off GOBIN="$(BINDIR)" \
		$(GO) -C oracle install -mod=readonly -trimpath \
		-ldflags '-w -s -X github.com/gurufinglobal/guru/oracle/internal/version.Version=$(ORACLE_VERSION) -X github.com/gurufinglobal/guru/oracle/internal/version.Commit=$(COMMIT)' \
		$(ORACLE_MAIN_PKG)

$(BUILD_DIR)/:
	@mkdir -p $(BUILD_DIR)

.PHONY: all build build-gurud build-oracled build-linux install

###############################################################################
###                         Dependencies & Verification                     ###
###############################################################################

mod-verify:
	@$(GO) mod verify

tidy-check:
	@$(GO) mod tidy -diff

tidy:
	@$(GO) mod tidy

.PHONY: mod-verify tidy-check tidy

###############################################################################
###                            Tests & Benchmarks                           ###
###############################################################################

TEST_PACKAGES ?= ./...
TEST_TIMEOUT ?= 15m
EXTRA_ARGS ?=

test: test-unit test-oracle

test-unit:
	@$(GO) test -mod=readonly -timeout=$(TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(TEST_PACKAGES)

test-oracle:
	@GOWORK=off $(GO) -C oracle test -mod=readonly -timeout=$(TEST_TIMEOUT) \
		$(EXTRA_ARGS) ./...

test-race:
	@$(GO) test -race -mod=readonly -timeout=$(TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(TEST_PACKAGES)

test-cover:
	@$(GO) test -mod=readonly -timeout=$(TEST_TIMEOUT) \
		-covermode=atomic -coverprofile=coverage.txt \
		$(EXTRA_ARGS) $(TEST_PACKAGES)

benchmark:
	@$(GO) test -mod=readonly -timeout=$(TEST_TIMEOUT) \
		-run='^$$' -bench=. $(EXTRA_ARGS) $(TEST_PACKAGES)

.PHONY: test test-unit test-oracle test-race test-cover benchmark

###############################################################################
###                            Linting & Formatting                         ###
###############################################################################

GO_FILES := $(shell find app cmd config x oracle -type f -name '*.go' \
	-not -name '*.pb.go' -not -name '*.pb.gw.go' 2>/dev/null)

lint: lint-go

lint-go:
	@$(GO) vet -mod=readonly $(TEST_PACKAGES)

format:
	@gofmt -w $(GO_FILES)

format-check:
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following Go files are not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: lint lint-go format format-check

###############################################################################
###                                Protobuf                                 ###
###############################################################################

BUF_IMAGE ?= ghcr.io/cosmos/proto-builder:0.18.1
DOCKER_PROTO := docker run --rm -v $(CURDIR):/workspace --workdir /workspace --user 0 $(BUF_IMAGE)
DOCKER_BUF := $(DOCKER_PROTO) buf

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Downloading Protobuf dependencies"
	@$(DOCKER_BUF) dep update proto
	@echo "Generating Guru gogo Protobuf files"
	@$(DOCKER_PROTO) sh scripts/proto-gen.sh

proto-format:
	@$(DOCKER_BUF) format -w proto

proto-lint:
	@$(DOCKER_BUF) lint proto

.PHONY: proto-all proto-gen proto-format proto-lint

###############################################################################
###                              Local Node                                 ###
###############################################################################

LOCALNET_ARGS ?=

local-node localnet-start:
	@./local_node.sh $(LOCALNET_ARGS)

.PHONY: local-node localnet-start
