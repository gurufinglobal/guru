#!/usr/bin/make -f

.DEFAULT_GOAL := build

###############################################################################
###                           Module & Versioning                           ###
###############################################################################

GO ?= go
VERSION ?= $(shell (git describe --tags --always 2>/dev/null || echo dev) | sed 's/^v//')
COMMIT ?= $(shell git log -1 --format='%H' 2>/dev/null || echo unknown)
TMVERSION := $(shell $(GO) list -m github.com/cometbft/cometbft 2>/dev/null | sed 's:.* ::')

###############################################################################
###                          Directories & Binaries                         ###
###############################################################################

BINARY ?= gurud
BINDIR ?= $(shell $(GO) env GOPATH)/bin
BUILDDIR ?= $(CURDIR)/build
# Keep BUILD_DIR as a compatibility override for the original Guru Makefile.
BUILD_DIR ?= $(BUILDDIR)
MAIN_PKG := ./cmd/gurud

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

build: | $(BUILD_DIR)/
	@echo "Building $(BINARY) to $(BUILD_DIR)/$(BINARY)"
	@CGO_ENABLED="$(CGO_ENABLED)" $(GO) build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(BINARY) $(MAIN_PKG)

build-linux:
	@GOOS=linux GOARCH=amd64 $(MAKE) build

install:
	@echo "Installing $(BINARY) to $(BINDIR)"
	@CGO_ENABLED="$(CGO_ENABLED)" GOBIN="$(BINDIR)" \
		$(GO) install $(BUILD_FLAGS) $(MAIN_PKG)

$(BUILD_DIR)/:
	@mkdir -p $(BUILD_DIR)

.PHONY: all build build-linux install

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

test: test-unit

test-unit:
	@$(GO) test -mod=readonly -timeout=$(TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(TEST_PACKAGES)

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

.PHONY: test test-unit test-race test-cover benchmark

###############################################################################
###                            Linting & Formatting                         ###
###############################################################################

GO_FILES := $(shell find app cmd config -type f -name '*.go' 2>/dev/null)

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
###                              Local Node                                 ###
###############################################################################

LOCALNET_ARGS ?=

local-node localnet-start:
	@./local_node.sh $(LOCALNET_ARGS)

.PHONY: local-node localnet-start
