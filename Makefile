# krouter Makefile
#
# Common targets:
#   make build        — Build daemon + CLI binary
#   make build-gui    — Build Wails GUI
#   make test         — Run unit tests
#   make test-integration — Run integration tests
#   make lint         — Run golangci-lint
#   make fmt          — Run gofmt
#   make clean        — Remove build artifacts
#   make dev          — Run daemon in dev mode
#   make release      — Build release binaries via goreleaser

BINARY      := krouter
GUI_BINARY  := krouter-gui
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -s -w"

.PHONY: build build-gui test test-integration lint fmt clean dev release deps

# ── Build ────────────────────────────────────────────────────────────────────

build:
	@echo "Building $(BINARY) $(VERSION)..."
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/krouter

build-gui:
	@echo "Building $(GUI_BINARY) $(VERSION)..."
	cd cmd/krouter-gui && wails build -ldflags "$(LDFLAGS)"

# ── Test ─────────────────────────────────────────────────────────────────────

test:
	@echo "Running unit tests..."
	go test -race -coverprofile=coverage.out ./internal/...

test-integration:
	@echo "Running integration tests..."
	go test -race -tags=integration ./tests/integration/...

coverage: test
	go tool cover -html=coverage.out

# ── Lint / Format ────────────────────────────────────────────────────────────

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "Install golangci-lint: https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	gofmt -s -w .
	goimports -w .

# ── Dev / Run ────────────────────────────────────────────────────────────────

dev:
	@echo "Running daemon in dev mode..."
	go run ./cmd/krouter serve --log-level=debug

# ── Dependencies ─────────────────────────────────────────────────────────────

deps:
	@echo "Tidying dependencies..."
	go mod tidy
	go mod verify

# ── Release ──────────────────────────────────────────────────────────────────

release:
	@command -v goreleaser >/dev/null 2>&1 || { \
		echo "Install goreleaser: https://goreleaser.com/install/"; exit 1; }
	goreleaser release --clean

release-snapshot:
	goreleaser release --snapshot --clean

# ── Clean ────────────────────────────────────────────────────────────────────

clean:
	@echo "Cleaning..."
	rm -rf bin/ dist/ coverage.out
	cd cmd/krouter-gui && rm -rf build/

# ── Help ─────────────────────────────────────────────────────────────────────

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
