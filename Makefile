GO ?= go
BIN_DIR ?= bin
APP ?= mnemos

.PHONY: fmt lint test build cover check sqlc security

fmt:
	$(GO) fmt ./...

lint:
	golangci-lint run ./...
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(APP) ./cmd/mnemos

cover:
	coverctl check

security:
	nox scan cmd && nox scan internal; \
	nox diff cmd internal; \
	rm -f findings.json

sqlc:
	sqlc generate

check: fmt lint test build cover security
