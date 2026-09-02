#!/usr/bin/env bash
# Quick runner for local development (supports host go or docker)
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if command -v go >/dev/null 2>&1; then
    exec go run "${DIR}/cmd/azlens" "$@"
else
    exec docker run --rm -it -v "${DIR}:/app" -v "${HOME}/.azure:/root/.azure:ro" -w /app golang:1.23-alpine go run ./cmd/azlens "$@"
fi
