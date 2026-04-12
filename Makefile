GO ?= go
BIN_DIR ?= bin
APP ?= mnemos
MCP_APP ?= mnemos-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

.PHONY: fmt lint test build check sqlc install

fmt:
	$(GO) fmt ./...

lint:
	$(GO) vet ./...
	golangci-lint run

test:
	$(GO) test ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/$(APP) ./cmd/mnemos
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/$(MCP_APP) ./cmd/mnemos-mcp

install:
	$(GO) install $(LDFLAGS) ./cmd/mnemos
	$(GO) install $(LDFLAGS) ./cmd/mnemos-mcp

sqlc:
	sqlc generate

check: fmt lint test build
