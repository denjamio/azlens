.PHONY: all build install test lint clean

INSTALL_DIR ?= /usr/local/bin
DOCKER_BUILDER = docker run --rm -v $(PWD):/app -w /app golang:1.23-alpine

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS = -s -w \
	-X github.com/denjamio/azlens/cmd.Version=$(VERSION) \
	-X github.com/denjamio/azlens/cmd.Commit=$(COMMIT) \
	-X github.com/denjamio/azlens/cmd.Date=$(DATE)

all: build

# Build static binary using Go inside Docker (no host Go required)
build:
	@mkdir -p bin
	$(DOCKER_BUILDER) sh -c "CGO_ENABLED=0 GOOS=linux go build -ldflags='$(LDFLAGS)' -o bin/azlens ./cmd/azlens"
	@echo "✓ Compiled standalone binary: bin/azlens"

# Install directly into system PATH
install:
	./install.sh

# Run test suite inside Docker
test:
	$(DOCKER_BUILDER) go test -v ./...

# Run gofmt + go vet inside Docker (no host Go required)
lint:
	$(DOCKER_BUILDER) sh -c "test -z \"$$(gofmt -l .)\" || { echo 'gofmt issues found:'; gofmt -l .; exit 1; } && go vet ./..."

clean:
	rm -rf bin/
