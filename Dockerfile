# Multi-stage Dockerfile for azlens
# Stage 1: Development & Testing environment with Go 1.23, golangci-lint, and kql-guard
FROM golang:1.23-bookworm AS dev

# Install essential tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    make \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && git config --global --add safe.directory '*'

# Install official golangci-lint
COPY --from=golangci/golangci-lint:v1.61.0 /usr/bin/golangci-lint /usr/local/bin/golangci-lint

# Install official Microsoft kql-guard (supports x86_64 and arm64)
ARG KQL_GUARD_VERSION=v0.2.0
RUN set -e; \
    ARCH=$(uname -m); \
    case "$ARCH" in \
        x86_64)  KQL_ARCH="linux-x64" ;; \
        aarch64) KQL_ARCH="linux-arm64" ;; \
        *) echo "Unsupported architecture for kql-guard: $ARCH"; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/local/bin/kql-guard "https://github.com/microsoft/kql-guard/releases/download/${KQL_GUARD_VERSION}/kql-guard-${KQL_ARCH}" && \
    chmod +x /usr/local/bin/kql-guard

WORKDIR /app

# Pre-cache Go dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build the azlens binary into /go/bin
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /go/bin/azlens ./cmd/azlens

# Stage 2: Runtime image containing Azure CLI + compiled azlens binary
FROM mcr.microsoft.com/azure-cli:latest AS runtime

WORKDIR /app
COPY --from=dev /go/bin/azlens /usr/local/bin/azlens

ENTRYPOINT ["azlens"]
