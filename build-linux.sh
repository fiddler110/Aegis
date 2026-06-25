#!/usr/bin/env bash
# build-linux.sh — Build Aegis and install it on Linux.
#
# Install order (first writable location wins):
#   1. /usr/local/bin/aegis  — system-wide, using sudo if required
#   2. $HOME/go/bin/aegis    — user-local fallback (no sudo needed)
#
# Usage:
#   chmod +x build-linux.sh
#   ./build-linux.sh

set -euo pipefail

# ── Locate Go ──────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
    echo "Error: Go is not installed or not in PATH." >&2
    echo "Install from: https://go.dev/dl/" >&2
    exit 1
fi

echo "Using $(go version)"

# ── Resolve version string ─────────────────────────────────────────────────────
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

# ── Build to a local binary first ─────────────────────────────────────────────
echo ""
echo "Building aegis ${VERSION}..."

LDFLAGS="-s -w -X github.com/scottymacleod/aegis/internal/cli.Version=${VERSION}"
go build -ldflags "${LDFLAGS}" -o ./aegis ./cmd/aegis

# ── Determine install directory ────────────────────────────────────────────────
# Try /usr/local/bin first. If the current user can't write there directly,
# attempt to use sudo. If sudo is not available or fails, fall back to ~/go/bin.

SYSTEM_BIN="/usr/local/bin"
USER_BIN="${HOME}/go/bin"
INSTALL_DIR=""
USE_SUDO=false

if [ -w "${SYSTEM_BIN}" ]; then
    # Running as root or the directory is already writable.
    INSTALL_DIR="${SYSTEM_BIN}"
elif command -v sudo &>/dev/null && sudo -n true 2>/dev/null; then
    # sudo is available and doesn't need a password right now.
    INSTALL_DIR="${SYSTEM_BIN}"
    USE_SUDO=true
elif command -v sudo &>/dev/null; then
    # sudo is available but will prompt — ask the user once.
    echo ""
    echo "Installing to ${SYSTEM_BIN} requires elevated privileges."
    if sudo true; then
        INSTALL_DIR="${SYSTEM_BIN}"
        USE_SUDO=true
    else
        echo "sudo failed; falling back to ${USER_BIN}"
        INSTALL_DIR="${USER_BIN}"
    fi
else
    echo "sudo not available; installing to ${USER_BIN}"
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
        echo "  Add this to your ~/.bashrc or ~/.profile:"
        echo "    export PATH=\"\${HOME}/go/bin:\${PATH}\""
    fi
    echo ""
fi

echo "Next steps:"
echo "  1. Run: aegis --first-init"
echo "  2. Run: export OPENAI_API_KEY=ollama   (for Ollama; see config for other providers)"
echo "  3. Run: aegis"
