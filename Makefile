#!/usr/bin/make -f

.DEFAULT_GOAL := build

###############################################################################
###                           Module & Versioning                           ###
###############################################################################

GO ?= go
DOCKER ?= docker
GOFMT ?= gofmt
VERSION ?= $(shell (git describe --tags --always 2>/dev/null || echo dev) | sed 's/^v//')
ORACLE_VERSION ?= $(shell value=$$(git describe --tags --match 'oracle/v[0-9]*' --always 2>/dev/null); if test -n "$$value"; then printf '%s\n' "$$value" | sed 's|^oracle/||'; else echo dev; fi)
COMMIT ?= $(shell git log -1 --format='%H' 2>/dev/null || echo unknown)
TMVERSION = $(shell $(GO) list -m github.com/cometbft/cometbft 2>/dev/null | sed 's:.* ::')

###############################################################################
###                          Directories & Binaries                         ###
###############################################################################

BINARY ?= gurud
ORACLE_BINARY ?= oracled
BINDIR ?= $(shell $(GO) env GOPATH)/bin
BUILDDIR ?= $(CURDIR)/build
# Keep BUILD_DIR as a compatibility override for the original Guru Makefile.
BUILD_DIR ?= $(BUILDDIR)
BUILD_DIR := $(abspath $(strip $(BUILD_DIR)))
MAIN_PKG := ./cmd/gurud
ORACLE_MAIN_PKG := ./cmd/oracled
VERSION_SMOKE_ROOT ?= $(BUILD_DIR)/version-smoke
VERSION_SMOKE_GURUD_VERSION ?= 0.0.0-gurud-smoke
VERSION_SMOKE_ORACLE_VERSION ?= 0.0.0-oracled-smoke
VERSION_SMOKE_COMMIT ?= 0123456789abcdef0123456789abcdef01234567

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

ifeq (staticlink,$(findstring staticlink,$(COSMOS_BUILD_OPTIONS)))
  ldflags += -linkmode external -extldflags '-static'
endif

BUILD_FLAGS = -mod=readonly -tags "$(build_tags)" -ldflags '$(strip $(ldflags))'

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
		-o "$(BUILD_DIR)/$(BINARY)" $(MAIN_PKG)

build-oracled: | $(BUILD_DIR)/
	@echo "Building $(ORACLE_BINARY) to $(BUILD_DIR)/$(ORACLE_BINARY)"
	@CGO_ENABLED=0 GOWORK=off $(GO) -C oracle build -mod=readonly -trimpath \
		-ldflags '-w -s -X github.com/gurufinglobal/guru/oracle/internal/version.Version=$(ORACLE_VERSION) -X github.com/gurufinglobal/guru/oracle/internal/version.Commit=$(COMMIT)' \
		-o $(abspath $(BUILD_DIR)/$(ORACLE_BINARY)) $(ORACLE_MAIN_PKG)

build-linux:
	@if [ "$$(uname -m)" = "aarch64" ] || [ "$$(uname -m)" = "arm64" ]; then \
	  echo "Building for linux/arm64..."; \
	  GOOS=linux GOARCH=arm64 CGO_ENABLED=1 $(MAKE) build; \
	else \
	  echo "Building for linux/amd64..."; \
	  CC=x86_64-linux-gnu-gcc GOOS=linux GOARCH=amd64 CGO_ENABLED=1 $(MAKE) build; \
	fi

install:
	@echo "Installing $(BINARY) to $(BINDIR)"
	@CGO_ENABLED="$(CGO_ENABLED)" GOBIN="$(BINDIR)" \
		$(GO) install $(BUILD_FLAGS) $(MAIN_PKG)
	@echo "Installing $(ORACLE_BINARY) to $(BINDIR)"
	@CGO_ENABLED=0 GOWORK=off GOBIN="$(BINDIR)" \
		$(GO) -C oracle install -mod=readonly -trimpath \
		-ldflags '-w -s -X github.com/gurufinglobal/guru/oracle/internal/version.Version=$(ORACLE_VERSION) -X github.com/gurufinglobal/guru/oracle/internal/version.Commit=$(COMMIT)' \
		$(ORACLE_MAIN_PKG)

version-smoke:
	@mkdir -p "$(VERSION_SMOKE_ROOT)"
	@set -eu; \
	scratch=$$(mktemp -d "$(VERSION_SMOKE_ROOT)/run.XXXXXX"); \
	trap 'rm -rf "$$scratch"' 0 1 2 15; \
	$(MAKE) --no-print-directory build \
		BUILD_DIR="$$scratch" \
		VERSION="$(VERSION_SMOKE_GURUD_VERSION)" \
		ORACLE_VERSION="$(VERSION_SMOKE_ORACLE_VERSION)" \
		COMMIT="$(VERSION_SMOKE_COMMIT)"; \
	gurud_output="$$("$$scratch/$(BINARY)" version --long --output json)"; \
	gurud_compact=$$(printf '%s\n' "$$gurud_output" | tr -d '[:space:]'); \
	printf '%s\n' "$$gurud_compact" | grep -Fq '"server_name":"$(BINARY)"' || { echo "gurud server_name mismatch" >&2; exit 1; }; \
	printf '%s\n' "$$gurud_compact" | grep -Fq '"version":"$(VERSION_SMOKE_GURUD_VERSION)"' || { echo "gurud version mismatch" >&2; exit 1; }; \
	printf '%s\n' "$$gurud_compact" | grep -Fq '"commit":"$(VERSION_SMOKE_COMMIT)"' || { echo "gurud commit mismatch" >&2; exit 1; }; \
	oracled_output="$$("$$scratch/$(ORACLE_BINARY)" --version)"; \
	expected="$(ORACLE_BINARY) version $(VERSION_SMOKE_ORACLE_VERSION) ($(VERSION_SMOKE_COMMIT))"; \
	test "$$oracled_output" = "$$expected" || { echo "oracled version mismatch" >&2; exit 1; }

$(BUILD_DIR)/:
	@mkdir -p "$(BUILD_DIR)"

.PHONY: all build build-gurud build-oracled build-linux install version-smoke

###############################################################################
###                         Dependencies & Verification                     ###
###############################################################################

mod-verify:
	@$(GO) mod verify

tidy-check:
	@$(GO) mod tidy -diff

tidy:
	@$(GO) mod tidy

mod-check-root:
	@$(GO) mod verify
	@$(GO) mod tidy -diff

mod-check-oracle:
	@GOWORK=off $(GO) -C oracle mod verify
	@GOWORK=off $(GO) -C oracle mod tidy -diff

mod-check: mod-check-root mod-check-oracle

.PHONY: mod-verify tidy-check tidy mod-check-root mod-check-oracle mod-check

###############################################################################
###                            Tests & Benchmarks                           ###
###############################################################################

TEST_PACKAGES ?= ./...
TEST_TIMEOUT ?= 15m
EXTRA_ARGS ?=
ROOT_TEST_PACKAGES ?= $(TEST_PACKAGES)
ORACLE_TEST_PACKAGES ?= ./...
ROOT_TEST_TIMEOUT ?= $(TEST_TIMEOUT)
ORACLE_TEST_TIMEOUT ?= $(TEST_TIMEOUT)
ROOT_TEST_ARGS ?=
ORACLE_TEST_ARGS ?=
COVERAGE_DIR ?= $(BUILD_DIR)/coverage
ROOT_COVERAGE_PROFILE ?= $(COVERAGE_DIR)/root.out
ORACLE_COVERAGE_PROFILE ?= $(COVERAGE_DIR)/oracle.out
COVERAGE_MODE ?= atomic
FUZZ_TIME ?= 30s
ROOT_BENCH_PACKAGES ?= $(ROOT_TEST_PACKAGES)
ORACLE_BENCH_PACKAGES ?= $(ORACLE_TEST_PACKAGES)
BENCH_TIMEOUT ?= $(TEST_TIMEOUT)
BENCH_ARGS ?=

test: test-unit

test-unit: test-unit-root test-unit-oracle

test-oracle: test-unit-oracle

test-unit-root:
	@$(GO) test -mod=readonly -timeout=$(ROOT_TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(ROOT_TEST_ARGS) $(ROOT_TEST_PACKAGES)

test-unit-oracle:
	@GOWORK=off $(GO) -C oracle test -mod=readonly -timeout=$(ORACLE_TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(ORACLE_TEST_ARGS) $(ORACLE_TEST_PACKAGES)

test-race: test-race-root test-race-oracle

test-race-root:
	@$(GO) test -race -mod=readonly -timeout=$(ROOT_TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(ROOT_TEST_ARGS) $(ROOT_TEST_PACKAGES)

test-race-oracle:
	@GOWORK=off $(GO) -C oracle test -race -mod=readonly -timeout=$(ORACLE_TEST_TIMEOUT) \
		$(EXTRA_ARGS) $(ORACLE_TEST_ARGS) $(ORACLE_TEST_PACKAGES)

test-cover: test-cover-root test-cover-oracle

test-unit-cover: test-cover

test-cover-root:
	@mkdir -p "$(dir $(ROOT_COVERAGE_PROFILE))"
	@$(GO) test -mod=readonly -timeout=$(ROOT_TEST_TIMEOUT) \
		-covermode=$(COVERAGE_MODE) -coverprofile="$(ROOT_COVERAGE_PROFILE)" \
		$(EXTRA_ARGS) $(ROOT_TEST_ARGS) $(ROOT_TEST_PACKAGES)

test-cover-oracle:
	@mkdir -p "$(dir $(ORACLE_COVERAGE_PROFILE))"
	@GOWORK=off $(GO) -C oracle test -mod=readonly -timeout=$(ORACLE_TEST_TIMEOUT) \
		-covermode=$(COVERAGE_MODE) -coverprofile="$(abspath $(ORACLE_COVERAGE_PROFILE))" \
		$(EXTRA_ARGS) $(ORACLE_TEST_ARGS) $(ORACLE_TEST_PACKAGES)

test-fuzz-oracle:
	@$(MAKE) --no-print-directory test-fuzz-oracle-decimal
	@$(MAKE) --no-print-directory test-fuzz-oracle-json

test-fuzz-oracle-decimal:
	@GOWORK=off $(GO) -C oracle test -mod=readonly -timeout=$(ORACLE_TEST_TIMEOUT) \
		-run='^$$' -fuzz='^FuzzNormalizeDecimal$$' -fuzztime=$(FUZZ_TIME) \
		$(EXTRA_ARGS) $(ORACLE_TEST_ARGS) ./internal/domain

test-fuzz-oracle-json:
	@GOWORK=off $(GO) -C oracle test -mod=readonly -timeout=$(ORACLE_TEST_TIMEOUT) \
		-run='^$$' -fuzz='^FuzzExtractJSONNumericText$$' -fuzztime=$(FUZZ_TIME) \
		$(EXTRA_ARGS) $(ORACLE_TEST_ARGS) ./internal/collector

benchmark: benchmark-root

benchmark-root:
	@$(GO) test -mod=readonly -timeout=$(BENCH_TIMEOUT) -run='^$$' -bench=. \
		$(EXTRA_ARGS) $(BENCH_ARGS) $(ROOT_BENCH_PACKAGES)

benchmark-oracle:
	@GOWORK=off $(GO) -C oracle test -mod=readonly -timeout=$(BENCH_TIMEOUT) \
		-run='^$$' -bench=. $(EXTRA_ARGS) $(BENCH_ARGS) $(ORACLE_BENCH_PACKAGES)

benchmark-all:
	@$(MAKE) --no-print-directory benchmark-root
	@$(MAKE) --no-print-directory benchmark-oracle

.PHONY: test test-unit test-unit-root test-unit-oracle test-oracle \
	test-race test-race-root test-race-oracle \
	test-cover test-unit-cover test-cover-root test-cover-oracle \
	test-fuzz-oracle test-fuzz-oracle-decimal test-fuzz-oracle-json \
	benchmark benchmark-root benchmark-oracle benchmark-all

###############################################################################
###                            Linting & Formatting                         ###
###############################################################################

GO_FILES := $(shell find app cmd config x oracle -type f -name '*.go' \
	-not -name '*.pb.go' -not -name '*.pb.gw.go' 2>/dev/null)

lint: lint-root lint-oracle

lint-go: lint-root

lint-root:
	@$(GO) vet -mod=readonly $(ROOT_TEST_PACKAGES)

lint-oracle:
	@GOWORK=off $(GO) -C oracle vet -mod=readonly $(ORACLE_TEST_PACKAGES)

format:
	@$(GOFMT) -w $(GO_FILES)

format-check:
	@set -eu; \
	set -o pipefail; \
	tmp=$$(mktemp); \
	if ! $(GOFMT) -l $(GO_FILES) > "$$tmp"; then \
		echo "gofmt failed:" >&2; \
		cat "$$tmp" >&2; \
		rm -f "$$tmp"; \
		exit 1; \
	fi; \
	if [ -s "$$tmp" ]; then \
		echo "The following Go files are not formatted:"; \
		cat "$$tmp"; \
		rm -f "$$tmp"; \
		exit 1; \
	fi; \
	rm -f "$$tmp"

.PHONY: lint lint-go lint-root lint-oracle format format-check

###############################################################################
###                                Protobuf                                 ###
###############################################################################

protoVer=0.14.0
protoImageName=ghcr.io/cosmos/proto-builder:$(protoVer)
protoImageDigest=sha256:93e2035b90e5780b4d56210a88ecb0afed881c7bb828285d4a61a897cebb54fb
protoImageRef=$(protoImageName)@$(protoImageDigest)

# Compatibility override for callers that provide the complete image reference.
PROTO_BUILDER_IMAGE ?= $(protoImageRef)
PROTO_UID ?= $(shell id -u)
PROTO_GID ?= $(shell id -g)
PROTO_SCRATCH_ROOT ?= $(BUILD_DIR)/proto
PROTO_HOME_DIR ?= $(PROTO_SCRATCH_ROOT)/home
PROTO_BREAKING_AGAINST ?=
protoImage=$(DOCKER) run --rm -v "$(CURDIR):/workspace" --workdir /workspace \
	--user "$(PROTO_UID):$(PROTO_GID)" --env HOME=/home/proto \
	-v "$(PROTO_HOME_DIR):/home/proto" $(PROTO_BUILDER_IMAGE)
protoImageReadOnly=$(DOCKER) run --rm -v "$(CURDIR):/workspace:ro" --workdir /workspace \
	--user "$(PROTO_UID):$(PROTO_GID)" --env HOME=/home/proto \
	-v "$(PROTO_HOME_DIR):/home/proto" $(PROTO_BUILDER_IMAGE)

# Cosmos EVM v0.6.2 additionally runs yoheimuta/protolint:0.44.0 with its
# repository-level .protolint.yml. Guru has no equivalent config; adopting
# those schema comment rules is a separate Proto source compatibility scope.

proto-all:
	@$(MAKE) --no-print-directory proto-format
	@$(MAKE) --no-print-directory proto-lint
	@$(MAKE) --no-print-directory proto-gen

proto-gen: | $(PROTO_HOME_DIR)/
	@echo "Generating Guru gogo Protobuf files"
	@$(protoImage) sh scripts/proto-gen.sh

proto-format: | $(PROTO_HOME_DIR)/
	@$(protoImage) buf format -w proto

proto-format-check: | $(PROTO_HOME_DIR)/
	@$(protoImageReadOnly) buf format --diff --exit-code proto

proto-lint: | $(PROTO_HOME_DIR)/
	@$(protoImageReadOnly) buf lint proto

proto-deps-update: | $(PROTO_HOME_DIR)/
	@$(protoImage) buf mod update proto

proto-drift: | $(PROTO_HOME_DIR)/
	@mkdir -p "$(PROTO_SCRATCH_ROOT)"
	@set -eu; \
	scratch=$$(mktemp -d "$(PROTO_SCRATCH_ROOT)/drift.XXXXXX"); \
	trap 'rm -rf "$$scratch"' 0 1 2 15; \
	cp -R proto scripts x "$$scratch/"; \
	$(DOCKER) run --rm --user "$(PROTO_UID):$(PROTO_GID)" \
		--env HOME=/home/proto -v "$(PROTO_HOME_DIR):/home/proto" \
		-v "$$scratch:/workspace" --workdir /workspace \
		$(PROTO_BUILDER_IMAGE) sh scripts/proto-gen.sh; \
	diff -ru "$(CURDIR)/x" "$$scratch/x"

proto-breaking: | $(PROTO_HOME_DIR)/
	@test -n "$(strip $(PROTO_BREAKING_AGAINST))" || { echo "PROTO_BREAKING_AGAINST is required" >&2; exit 2; }
	@$(protoImageReadOnly) buf breaking proto --against "$(PROTO_BREAKING_AGAINST)"

proto-check:
	@$(MAKE) --no-print-directory proto-format-check
	@$(MAKE) --no-print-directory proto-lint
	@$(MAKE) --no-print-directory proto-drift

$(PROTO_HOME_DIR)/:
	@mkdir -p "$@"

.PHONY: proto-all proto-gen proto-format proto-format-check proto-lint \
	proto-deps-update proto-drift proto-breaking proto-check

###############################################################################
###                           Default Verification                          ###
###############################################################################

verify:
	@$(MAKE) --no-print-directory mod-check
	@$(MAKE) --no-print-directory format-check
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory test-unit

.PHONY: verify

###############################################################################
###                              Local Node                                 ###
###############################################################################

LOCALNET_ARGS ?=

local-node localnet-start:
	@./local_node.sh $(LOCALNET_ARGS)

.PHONY: local-node localnet-start
