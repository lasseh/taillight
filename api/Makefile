BINARY_NAME ?= taillight
GO ?= go
GOFLAGS ?=
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags="-X main.Version=$(VERSION)"

# Detect gotestsum
GOTESTSUM := $(shell command -v gotestsum 2>/dev/null)
ifdef GOTESTSUM
  TEST_CMD := gotestsum --format testdox --
else
  TEST_CMD := $(GO) test
endif

.PHONY: test-integration build build-linux build-shipper build-tui build-tui-linux build-wish build-wish-linux test lint fix tidy vulncheck generate coverage clean help migrate-up migrate-down migrate-version

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build
build: ## Build binary
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME) ./cmd/taillight

build-linux: ## Build linux binary
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 ./cmd/taillight

build-shipper: ## Build taillight-shipper tool
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o taillight-shipper ./cmd/taillight-shipper

build-tui: ## Build taillight-tui
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o taillight-tui ./cmd/taillight-tui

build-tui-linux: ## Build taillight-tui for Linux amd64
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o taillight-tui-linux-amd64 ./cmd/taillight-tui

build-wish: ## Build taillight-wish SSH server
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o taillight-wish ./cmd/taillight-wish

build-wish-linux: ## Build taillight-wish for Linux amd64
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o taillight-wish-linux-amd64 ./cmd/taillight-wish

##@ Quality
test: ## Run tests (DB integration tests skip unless TEST_DATABASE_URL is set)
	$(TEST_CMD) -race ./...

INTEGRATION_IMAGE ?= timescale/timescaledb:latest-pg18
INTEGRATION_PORT  ?= 55432

test-integration: ## Run DB integration tests in an ephemeral TimescaleDB (or against $$TEST_DATABASE_URL)
ifdef TEST_DATABASE_URL
	$(TEST_CMD) -race -run Integration ./internal/postgres/...
else
	@echo "starting ephemeral $(INTEGRATION_IMAGE) on port $(INTEGRATION_PORT)..."
	@cid=$$(docker run -d --rm -e POSTGRES_USER=taillight -e POSTGRES_PASSWORD=taillight \
		-e POSTGRES_DB=taillight_test -p $(INTEGRATION_PORT):5432 $(INTEGRATION_IMAGE)); \
	trap 'docker stop $$cid >/dev/null 2>&1 || true' EXIT; \
	echo "waiting for postgres to accept connections..."; \
	for i in $$(seq 1 60); do \
		docker exec $$cid pg_isready -U taillight -d taillight_test >/dev/null 2>&1 && break; sleep 1; \
	done; \
	TEST_DATABASE_URL="postgres://taillight:taillight@127.0.0.1:$(INTEGRATION_PORT)/taillight_test?sslmode=disable" \
		$(TEST_CMD) -race -run Integration ./internal/postgres/...
endif

lint: ## Run linter (includes vet, fmt, imports via golangci-lint)
	golangci-lint run ./...

coverage: ## Run tests with coverage
	$(TEST_CMD) -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

##@ Utility
fix: ## Apply Go version modernizations (go fix)
	$(GO) fix ./...

tidy: ## Verify go.mod and go.sum are tidy
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy" && exit 1)

vulncheck: ## Scan for known vulnerabilities
	govulncheck ./...

generate: ## Verify generated code is up to date
	$(GO) generate ./...
	@git diff --exit-code || (echo "generated code is out of date" && exit 1)

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux-amd64 taillight-shipper taillight-tui taillight-tui-linux-amd64 taillight-wish taillight-wish-linux-amd64 coverage.out coverage.html

##@ Database
migrate-up: build ## Apply all pending migrations
	./$(BINARY_NAME) migrate up

migrate-down: build ## Roll back one migration
	./$(BINARY_NAME) migrate down --steps 1

migrate-version: build ## Show current migration version
	./$(BINARY_NAME) migrate version

.DEFAULT_GOAL := help
