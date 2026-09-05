#!/usr/bin/env bash
set -e -o pipefail

# ==============================================================================
# AzLens Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/denjamio/azlens/main/install.sh | bash
# ==============================================================================

REPO="denjamio/azlens"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
AZLENS_VERSION="${AZLENS_VERSION:-latest}"
USE_SUDO=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${CYAN}🚀 Installing AzLens (Azure Actionable Telemetry & Regression CLI)...${NC}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
  linux*)  OS="linux" ;;
  darwin*) OS="darwin" ;;
  msys*|mingw*|cygwin*) OS="windows" ;;
  *)
    echo -e "${RED}❌ Unsupported Operating System: ${OS}${NC}"
    exit 1
    ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo -e "${RED}❌ Unsupported Architecture: ${ARCH}${NC}"
    exit 1
    ;;
esac

echo -e "Detected platform: ${YELLOW}${OS}/${ARCH}${NC}"

# Determine if sudo is needed for installation directory
if [ ! -w "${INSTALL_DIR}" ]; then
  if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
      USE_SUDO="sudo"
    else
      INSTALL_DIR="${HOME}/.local/bin"
      mkdir -p "${INSTALL_DIR}"
    fi
  fi
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

BINARY_INSTALLED=false

# 1. Try to download from GitHub Releases (with SHA256 checksum verification)
echo -e "${CYAN}🔍 Checking release from GitHub (${REPO})...${NC}"
RELEASE_API="https://api.github.com/repos/${REPO}/releases/latest"
if [ "${AZLENS_VERSION}" != "latest" ]; then
  RELEASE_API="https://api.github.com/repos/${REPO}/releases/tags/${AZLENS_VERSION}"
fi
LATEST_TAG=$(curl -sSL -H "Accept: application/vnd.github.v3+json" "${RELEASE_API}" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || true)

if [ -n "${LATEST_TAG}" ]; then
  TARBALL="azlens_${OS}_${ARCH}.tar.gz"
  BASE_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}"
  DOWNLOAD_URL="${BASE_URL}/${TARBALL}"
  echo -e "⬇️  Downloading ${DOWNLOAD_URL}..."

  if curl -sSL -f -o "${TMP_DIR}/${TARBALL}" "${DOWNLOAD_URL}" 2>/dev/null; then
    CHECKSUMS_OK=false
    if curl -sSL -f -o "${TMP_DIR}/checksums.txt" "${BASE_URL}/checksums.txt" 2>/dev/null; then
      EXPECTED_SHA=$(grep -E " [0-9a-fA-F]{64} +${TARBALL}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}' | head -n1 || true)
      if [ -n "${EXPECTED_SHA}" ]; then
        if command -v sha256sum >/dev/null 2>&1; then
          ACTUAL_SHA=$(sha256sum "${TMP_DIR}/${TARBALL}" | awk '{print $1}')
        elif command -v shasum >/dev/null 2>&1; then
          ACTUAL_SHA=$(shasum -a 256 "${TMP_DIR}/${TARBALL}" | awk '{print $1}')
        fi
        if [ -n "${ACTUAL_SHA}" ] && [ "${ACTUAL_SHA}" = "${EXPECTED_SHA}" ]; then
          CHECKSUMS_OK=true
          echo -e "${GREEN}✓ SHA256 checksum verified${NC}"
        else
          echo -e "${RED}❌ Checksum mismatch for ${TARBALL} (expected ${EXPECTED_SHA}, got ${ACTUAL_SHA}).${NC}"
          echo -e "${RED}   The download may be corrupted or tampered with. Aborting.${NC}"
          exit 1
        fi
      else
        echo -e "${RED}❌ No checksum entry found for ${TARBALL} in checksums.txt. Aborting.${NC}"
        exit 1
      fi
    else
      echo -e "${RED}❌ checksums.txt not available for ${LATEST_TAG}. Aborting.${NC}"
      exit 1
    fi

    if [ "${CHECKSUMS_OK}" = true ]; then
      tar -xzf "${TMP_DIR}/${TARBALL}" -C "${TMP_DIR}"
      ${USE_SUDO} install -d "${INSTALL_DIR}"
      ${USE_SUDO} install -m 755 "${TMP_DIR}/azlens" "${INSTALL_DIR}/azlens"
      BINARY_INSTALLED=true
      echo -e "${GREEN}✓ Installed pre-built binary ${LATEST_TAG} to ${INSTALL_DIR}/azlens${NC}"
    fi
  fi
fi

# 2. Fallback: Build via Docker if running from cloned repository or if pre-built release isn't accessible
if [ "${BINARY_INSTALLED}" = false ]; then
  if [ -f "go.mod" ] && command -v docker >/dev/null 2>&1; then
    echo -e "${YELLOW}ℹ️  Pre-built binary not found. Compiling via Docker builder...${NC}"
    mkdir -p bin
    docker run --rm -v "$(pwd)":/app -w /app golang:1.23-alpine sh -c \
      "CGO_ENABLED=0 GOOS=${OS} GOARCH=${ARCH} go build -ldflags='-s -w' -o /app/bin/azlens ./cmd/azlens"
    
    ${USE_SUDO} install -d "${INSTALL_DIR}"
    ${USE_SUDO} install -m 755 bin/azlens "${INSTALL_DIR}/azlens"
    BINARY_INSTALLED=true
    echo -e "${GREEN}✓ Compiled and installed to ${INSTALL_DIR}/azlens${NC}"
  fi
fi

if [ "${BINARY_INSTALLED}" = false ]; then
  echo -e "${RED}❌ Installation failed. Please ensure Docker is running or check your internet connection.${NC}"
  exit 1
fi

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo -e "${YELLOW}⚠️  Warning: ${INSTALL_DIR} is not in your PATH.${NC}"
    echo -e "Add it by running: ${CYAN}export PATH=\"\$PATH:${INSTALL_DIR}\"${NC}"
    ;;
esac

echo -e "\n${GREEN}🎉 AzLens successfully installed!${NC}"
echo -e "Try running: ${CYAN}azlens --help${NC} or ${CYAN}azlens deploy 30m --mock${NC}\n"
