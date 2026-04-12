GO ?= go
BIN_DIR ?= bin
APP ?= mnemos

.PHONY: fmt lint test build check sqlc

fmt:
	$(GO) fmt ./...

lint:
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(APP) ./cmd/mnemos

sqlc:
	sqlc generate

check: fmt lint test build
