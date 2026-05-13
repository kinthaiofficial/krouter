# krouter Makefile
#
# Common targets:
#   make build            — Build daemon binary
#   make build-installer  — Build krouter-installer binary
#   make build-frontend   — Build both React frontends
#   make test             — Run unit tests
#   make test-integration — Run integration tests
#   make lint             — Run golangci-lint
#   make fmt              — Run gofmt
#   make clean            — Remove build artifacts
#   make dev              — Run daemon in dev mode
#   make package-macos    — Build macOS .dmg (macOS only)
#   make package-appimage — Build Linux .AppImage

BINARY     := krouter
INSTALLER  := krouter-installer
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -s -w"

.PHONY: build build-installer build-frontend test test-integration lint fmt clean dev \
        package-macos package-appimage deps help

# ── Build ────────────────────────────────────────────────────────────────────

build:
	@echo "Building $(BINARY) $(VERSION)..."
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/krouter

build-installer:
	@echo "Building $(INSTALLER) $(VERSION)..."
	go build $(LDFLAGS) -o bin/$(INSTALLER) ./cmd/krouter-installer

build-frontend:
	@echo "Building dashboard frontend..."
	cd frontend && npm ci && npm run build
	@echo "Building install wizard frontend..."
	cd frontend-install && npm ci && npm run build

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

# ── Packaging ────────────────────────────────────────────────────────────────

package-macos: build build-installer
	@echo "Packaging macOS DMG..."
	@[ "$$(uname)" = "Darwin" ] || (echo "Error: macOS required for .dmg packaging" && exit 1)
	cp bin/$(BINARY)    dist/krouter-apple-macos
	cp bin/$(INSTALLER) dist/krouter-installer-apple-macos
	VERSION=$(VERSION) DIST=dist bash packaging/macos/build-dmg.sh

package-appimage: build build-installer
	@echo "Packaging Linux AppImage..."
	mkdir -p dist
	cp bin/$(BINARY)    dist/krouter-linux-amd64
	cp bin/$(INSTALLER) dist/krouter-installer-linux-amd64
	VERSION=$(VERSION) ARCH=x86_64 DIST=dist bash packaging/appimage/build.sh

# ── Dependencies ─────────────────────────────────────────────────────────────

deps:
	@echo "Tidying dependencies..."
	go mod tidy
	go mod verify

# ── Clean ────────────────────────────────────────────────────────────────────

clean:
	@echo "Cleaning..."
	rm -rf bin/ dist/ coverage.out

# ── Help ─────────────────────────────────────────────────────────────────────

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
