GO ?= go
BINARY := gurud
BUILD_DIR := build
VERSION ?= dev
COMMIT ?= unknown

LDFLAGS := \
	-X github.com/cosmos/cosmos-sdk/version.Name=guru \
	-X github.com/cosmos/cosmos-sdk/version.AppName=$(BINARY) \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

.PHONY: build format test test-race

build:
	mkdir -p $(BUILD_DIR)
	$(GO) build -mod=readonly -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/gurud

format:
	gofmt -w app/*.go config/*.go cmd/gurud/*.go cmd/gurud/cmd/*.go

test:
	$(GO) test -mod=readonly ./...

test-race:
	$(GO) test -race -mod=readonly ./...
