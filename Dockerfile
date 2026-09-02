# Multi-stage Dockerfile for azlens
# Stage 1: Build & Development environment with Go + Azure CLI
FROM golang:1.23-alpine AS dev

# Install essential tools, git, and python/pip for Azure CLI
RUN apk add --no-cache \
    bash \
    curl \
    git \
    make \
    gcc \
    musl-dev \
    py3-pip \
    python3-dev \
    libffi-dev \
    openssl-dev \
    cargo

# Install Azure CLI inside the container (pinned for reproducible builds)
RUN pip install --no-cache-dir --break-system-packages azure-cli==2.90.0

WORKDIR /app

# Cache dependencies (errors fail the build instead of being swallowed)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build the azlens binary into /go/bin
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /go/bin/azlens ./cmd/azlens

# Stage 2: Runtime image containing az + compiled azlens binary
FROM python:3.11-alpine AS runtime

RUN apk add --no-cache bash curl ca-certificates && \
    pip install --no-cache-dir --break-system-packages azure-cli==2.90.0

WORKDIR /app
COPY --from=dev /go/bin/azlens /usr/local/bin/azlens

ENTRYPOINT ["azlens"]
