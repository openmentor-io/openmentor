.PHONY: help build build-migrate build-worker run run-worker test test-coverage test-race lint docker-build docker-run clean fmt fmt-check vet security ci pre-commit install-tools migrate migrate-build

# Pinned so `make lint` locally and the lint job in CI resolve to the SAME
# linter build. .github/workflows/ci-api.yml reads this value out of this file
# rather than hard-coding its own — an unpinned "@latest" on either side is how
# CI and local drift apart in the first place.
GOLANGCI_VERSION := v2.7.2

# The toolchain go.mod selects for this module — deliberately lazy (`=`) so it
# only runs for the target that needs it. `go install pkg@version` acts outside
# the main module and so picks a toolchain from the TARGET's go.mod, not ours;
# for golangci-lint that means a machine whose default go predates our `go`
# directive installs a linter that then refuses to lint us ("the Go language
# version used to build golangci-lint is lower than the targeted Go version").
GO_TOOLCHAIN = $(shell go version | awk '{print $$3}')

# Default target
help:
	@echo "Available targets:"
	@echo "  build          - Build the Go application"
	@echo "  build-worker   - Build the background worker binary"
	@echo "  run            - Run the application locally"
	@echo "  run-worker     - Run the background worker locally"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  test-race      - Run tests with race detection"
	@echo "  lint           - Run linters"
	@echo "  vet            - Run go vet"
	@echo "  fmt            - Format code"
	@echo "  fmt-check      - Check code formatting"
	@echo "  security       - Run security scanner (gosec)"
	@echo "  ci             - Run all CI checks"
	@echo "  pre-commit     - Run pre-commit checks"
	@echo "  install-tools  - Install development tools"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  clean          - Clean build artifacts"
	@echo "  deps           - Download dependencies"
	@echo "  tidy           - Tidy dependencies"
	@echo "  migrate        - Run database migrations"
	@echo "  migrate-build  - Build migration binary"

# Build the application
build:
	@echo "Building OpenMentor API..."
	@go build -o bin/openmentor-api cmd/api/main.go
	@go build -o bin/migrate cmd/migrate/main.go
	@go build -o bin/worker cmd/worker/main.go
	@echo "✅ Built: bin/openmentor-api, bin/migrate, bin/worker"

# Build the background worker binary
build-worker:
	@echo "Building OpenMentor worker..."
	@go build -o bin/worker cmd/worker/main.go
	@echo "✅ Built: bin/worker"

# Run the application
run:
	@echo "Running OpenMentor API..."
	@go run cmd/api/main.go

# Run the background worker
run-worker:
	@echo "Running OpenMentor worker..."
	@go run cmd/worker/main.go

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out
	@echo "\nTo view HTML coverage report, run: go tool cover -html=coverage.out"

# Run tests with race detection
test-race:
	@echo "Running tests with race detection..."
	@go test -v -race ./...

# Run linters. This is the single definition of "lint" for the API — CI runs
# the same golangci-lint at the same pinned version, driven by .golangci.yml.
# It subsumes gofmt/goimports (formatters block), go vet, staticcheck,
# errcheck, ineffassign and unused, so those need no separate step.
#
# The guards are the point: pinning only the CI installer would still let a
# developer's binary be a different build, and different builds report different
# issues — the drift this whole target exists to prevent. Two things have to
# hold. The version must be the pinned one. And the binary must have been built
# with a Go at least as new as the version go.mod targets, because golangci-lint
# refuses outright to load a config aimed at a newer Go than itself ("the Go
# language version used to build golangci-lint is lower than the targeted Go
# version") — a `go` directive bump breaks every already-installed linter.
#
# Candidates are tried in order and the first one satisfying both wins: GOPATH/
# bin (where install-tools puts it) comes first so a package-manager build on
# PATH cannot silently shadow the pinned one, but a good PATH binary still works
# when GOPATH/bin holds a stale one.
lint:
	@echo "Running linters..."
	@want=$$(echo "$(GOLANGCI_VERSION)" | sed 's/^v//'); \
	need=$$(awk '/^toolchain /{print $$2; found=1; exit} /^go /{v=$$2} END{if (!found) print v}' go.mod \
		| sed 's/^go//' | cut -d. -f1,2); \
	for bin in "$$(go env GOPATH)/bin/golangci-lint" golangci-lint; do \
		path=$$(command -v "$$bin" 2>/dev/null) || continue; \
		have=$$("$$path" version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
		if [ "$$have" != "$$want" ]; then \
			why="$$path is golangci-lint $$have, but this repo pins $$want"; continue; \
		fi; \
		built=$$(go version "$$path" 2>/dev/null | awk '{print $$2}' | sed 's/^go//' | cut -d. -f1,2); \
		if [ "$$(echo "$$built" | awk -F. '{print $$1*1000+$$2}')" -lt "$$(echo "$$need" | awk -F. '{print $$1*1000+$$2}')" ]; then \
			why="$$path was built with go$$built, older than the go$$need go.mod targets"; continue; \
		fi; \
		exec "$$path" run; \
	done; \
	echo "❌ no usable golangci-lint found."; \
	[ -n "$$why" ] && echo "   $$why"; \
	echo "   Run: make install-tools   (installs the pinned $(GOLANGCI_VERSION))"; \
	exit 1

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t openmentor-api:latest .

# Run Docker container
docker-run:
	@echo "Running Docker container..."
	@docker run -p 8081:8081 --env-file .env openmentor-api:latest

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf logs/
	@go clean

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod verify

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	@go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -w .

# Check formatting
fmt-check:
	@echo "Checking code formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "❌ Code is not formatted. Run 'make fmt'"; \
		gofmt -d .; \
		exit 1; \
	fi
	@echo "✅ All files are properly formatted"

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Run security scanner
security:
	@echo "Running security scanner..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed. Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest"; \
		exit 1; \
	fi


# Run all CI checks — mirrors the gating jobs in .github/workflows/ci-api.yml.
#
# lint (golangci-lint) already covers gofmt, go vet, staticcheck AND gosec, so
# none of those get a second invocation here. The `security` target below stays
# for a deeper standalone scan, but it is deliberately NOT part of `ci`: the
# gosec job in CI is informational (-no-fail, uploads SARIF) rather than a
# gate, so making it fail here would put local and CI back out of step.
ci: lint test-race
	@echo ""
	@echo "✅ All CI checks passed!"

# Pre-commit hook
pre-commit: fmt vet test
	@echo "✅ Pre-commit checks passed!"

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "✅ All tools installed (golangci-lint $(GOLANGCI_VERSION))!"

# Build migration binary
migrate-build:
	@echo "Building migration tool..."
	@go build -o bin/migrate cmd/migrate/main.go
	@echo "✅ Migration tool built at bin/migrate"

# Run database migrations
migrate: migrate-build
	@echo "Running database migrations..."
	@./scripts/migrate.sh
