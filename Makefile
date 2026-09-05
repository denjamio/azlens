.PHONY: all build install test lint check dev clean

COMPOSE ?= docker compose

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS = -s -w \
	-X github.com/denjamio/azlens/cmd.Version=$(VERSION) \
	-X github.com/denjamio/azlens/cmd.Commit=$(COMMIT) \
	-X github.com/denjamio/azlens/cmd.Date=$(DATE)

all: build

# Build standalone binary inside isolated container (no host tools required)
build:
	@mkdir -p bin
	$(COMPOSE) run --rm build
	@echo "✓ Compiled standalone binary: bin/azlens"

# Install directly into system PATH via install.sh
install:
	./install.sh

# Run unit tests + race detector + kql-guard AST validation inside container
test:
	$(COMPOSE) run --rm test

# Run golangci-lint inside container
lint:
	$(COMPOSE) run --rm lint

# Run full quality gate (linter + tests + kql-guard)
check:
	$(COMPOSE) run --rm check

# Open an interactive bash shell in the dev container
dev:
	$(COMPOSE) run --rm dev

clean:
	rm -rf bin/
