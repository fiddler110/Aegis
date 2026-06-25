#!/usr/bin/env bash
# build-macos.sh — Build Aegis and install it on macOS (Intel and Apple Silicon).
#
# Install order (first writable location wins):
#   1. /usr/local/bin/aegis  — works on both Intel and Apple Silicon Macs
#   2. $HOME/go/bin/aegis    — user-local fallback (no sudo needed)
#
# Usage:
#   chmod +x build-macos.sh
#   ./build-macos.sh

set -euo pipefail

# ── Locate Go ──────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
    echo "Error: Go is not installed or not in PATH." >&2
    echo "Install from: https://go.dev/dl/"  >&2
    echo "Or via Homebrew: brew install go" >&2
    exit 1
fi

echo "Using $(go version)"
echo "Architecture: $(uname -m)"

# ── Resolve version string ─────────────────────────────────────────────────────
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

# ── Build ──────────────────────────────────────────────────────────────────────
echo ""
echo "Building aegis ${VERSION}..."

LDFLAGS="-s -w -X github.com/scottymacleod/aegis/internal/cli.Version=${VERSION}"
go build -ldflags "${LDFLAGS}" -o ./aegis ./cmd/aegis

# ── Determine install directory ────────────────────────────────────────────────
# /usr/local/bin works on both Intel and Apple Silicon. On Apple Silicon,
# Homebrew lives in /opt/homebrew but /usr/local/bin is still valid for
# user-installed tools and is already in PATH via /etc/paths.

SYSTEM_BIN="/usr/local/bin"
USER_BIN="${HOME}/go/bin"
INSTALL_DIR=""
USE_SUDO=false

# Ensure /usr/local/bin exists (it always does on macOS, but guard anyway).
if [ ! -d "${SYSTEM_BIN}" ]; then
    sudo mkdir -p "${SYSTEM_BIN}"
fi

if [ -w "${SYSTEM_BIN}" ]; then
    INSTALL_DIR="${SYSTEM_BIN}"
elif command -v sudo &>/dev/null && sudo -n true 2>/dev/null; then
    INSTALL_DIR="${SYSTEM_BIN}"
    USE_SUDO=true
elif command -v sudo &>/dev/null; then
    echo ""
    echo "Installing to ${SYSTEM_BIN} requires your password (sudo)."
    if sudo true; then
        INSTALL_DIR="${SYSTEM_BIN}"
        USE_SUDO=true
    else
        echo "sudo cancelled; falling back to ${USER_BIN}"
        INSTALL_DIR="${USER_BIN}"
    fi
else
    INSTALL_DIR="${USER_BIN}"
fi

mkdir -p "${INSTALL_DIR}"
DEST="${INSTALL_DIR}/aegis"

# ── Install ────────────────────────────────────────────────────────────────────
if [ "${USE_SUDO}" = true ]; then
    sudo install -m 755 ./aegis "${DEST}"
else
    install -m 755 ./aegis "${DEST}"
fi

rm -f ./aegis   # clean up the local copy

# ── Done ───────────────────────────────────────────────────────────────────────
echo ""
echo "Installed: ${DEST}"
echo "Version:   ${VERSION}"
echo ""

# Warn if the install directory is not in PATH.
if ! echo "${PATH}" | tr ':' '\n' | grep -qx "${INSTALL_DIR}"; then
    echo "Warning: ${INSTALL_DIR} is not in your PATH."
    if [ "${INSTALL_DIR}" = "${USER_BIN}" ]; then
        echo "  Add this to your ~/.zshrc or ~/.bash_profile:"
        echo "    export PATH=\"\${HOME}/go/bin:\${PATH}\""
    fi
    echo ""
fi

echo "Next steps:"
echo "  1. Run: aegis --first-init"
echo "  2. Run: export OPENAI_API_KEY=ollama   (for Ollama; see config for other providers)"
echo "  3. Run: aegis"
