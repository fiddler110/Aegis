#!/usr/bin/env bash
# build-linux.sh — Build Aegis and set up your shell for first-time use on Linux.
#
# Two optional actions:
#   [1] Compile aegis and install it to /usr/local/bin (or ~/go/bin fallback)
#   [2] Add an aegis-config function to your shell's aliases file so you can
#       run "aegis-config" to open the Aegis config file in your editor
#
# Alias file priority (first existing file wins; file is created if none exist):
#   zsh  : ~/.zsh_aliases  ~/.zshrc_aliases  ~/.aliases  → ~/.zshrc
#   bash : ~/.bash_aliases  ~/.aliases                   → ~/.bashrc
#   fish : ~/.config/fish/functions/aegis-config.fish    (function file)
#   other: ~/.aliases                                    → ~/.profile
#
# Usage:
#   chmod +x build-linux.sh && ./build-linux.sh

set -uo pipefail

# ─── Colours (only when stdout is a terminal) ──────────────────────────────────
if [ -t 1 ] && command -v tput &>/dev/null && tput colors &>/dev/null; then
    BOLD=$(tput bold); CYAN=$(tput setaf 6); GREEN=$(tput setaf 2)
    YELLOW=$(tput setaf 3); DIM=$(tput setaf 8 2>/dev/null || tput dim); RESET=$(tput sgr0)
else
    BOLD=""; CYAN=""; GREEN=""; YELLOW=""; DIM=""; RESET=""
fi

divider() { echo "  ${DIM}$(printf '─%.0s' {1..66})${RESET}"; }
header()  { echo "  ${BOLD}${CYAN}$*${RESET}"; }
item()    { echo "    ${BOLD}$*${RESET}"; }
detail()  { echo "        ${DIM}$*${RESET}"; }
ok()      { echo "  ${GREEN}OK${RESET}  $*"; }
skip()    { echo "  ${DIM}--  $*${RESET}"; }
warn()    { echo "  ${YELLOW}!!${RESET}  $*"; }

# ─── Locate Go ─────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
    echo "Error: Go is not installed or not in PATH." >&2
    echo "Install from: https://go.dev/dl/" >&2
    exit 1
fi
GO_VER=$(go version)

# ─── Resolve binary install location ───────────────────────────────────────────
SYSTEM_BIN="/usr/local/bin"
# Use go env GOPATH so we respect a non-default GOPATH instead of assuming ~/go.
USER_BIN="$(go env GOPATH 2>/dev/null || echo "${HOME}/go")/bin"
INSTALL_DIR=""
USE_SUDO=false
BIN_EXISTS=false

if [ -w "${SYSTEM_BIN}" ]; then
    INSTALL_DIR="${SYSTEM_BIN}"
elif command -v sudo &>/dev/null; then
    # sudo is available; we'll prompt for a password at install time if needed,
    # rather than requiring a cached credential right now (sudo -n).
    INSTALL_DIR="${SYSTEM_BIN}"
    USE_SUDO=true
else
    INSTALL_DIR="${USER_BIN}"
fi
BIN_DEST="${INSTALL_DIR}/aegis"
[ -f "${BIN_DEST}" ] && BIN_EXISTS=true

# ─── Detect stale binary at a different location ───────────────────────────────
# If aegis is already on PATH but NOT at our install destination, we'll remove
# that old copy during action [1] so there is no ambiguity about which binary
# runs after installation.
EXISTING_BIN=$(command -v aegis 2>/dev/null || true)
[ "${EXISTING_BIN}" = "${BIN_DEST}" ] && EXISTING_BIN=""

# ─── Resolve git version ───────────────────────────────────────────────────────
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

# ─── Detect shell and choose alias file ────────────────────────────────────────
SHELL_NAME=$(basename "${SHELL:-/bin/sh}")
ALIAS_FILE=""
ALIAS_METHOD="append"   # "append" | "fish"

case "${SHELL_NAME}" in
    zsh)
        for f in "${HOME}/.zsh_aliases" "${HOME}/.zshrc_aliases" "${HOME}/.aliases"; do
            if [ -f "$f" ]; then ALIAS_FILE="$f"; break; fi
        done
        [ -z "${ALIAS_FILE}" ] && ALIAS_FILE="${HOME}/.zshrc"
        ;;
    bash)
        for f in "${HOME}/.bash_aliases" "${HOME}/.aliases"; do
            if [ -f "$f" ]; then ALIAS_FILE="$f"; break; fi
        done
        [ -z "${ALIAS_FILE}" ] && ALIAS_FILE="${HOME}/.bashrc"
        ;;
    fish)
        ALIAS_METHOD="fish"
        ALIAS_FILE="${HOME}/.config/fish/functions/aegis-config.fish"
        ;;
    *)
        for f in "${HOME}/.aliases"; do
            if [ -f "$f" ]; then ALIAS_FILE="$f"; break; fi
        done
        [ -z "${ALIAS_FILE}" ] && ALIAS_FILE="${HOME}/.profile"
        ;;
esac

# Check whether the function is already defined in the target file.
ALIAS_EXISTS=false
if [ "${ALIAS_METHOD}" = "fish" ]; then
    [ -f "${ALIAS_FILE}" ] && ALIAS_EXISTS=true
elif [ -f "${ALIAS_FILE}" ] && grep -q 'aegis-config' "${ALIAS_FILE}" 2>/dev/null; then
    ALIAS_EXISTS=true
fi

AEGIS_CONFIG_PATH="${HOME}/.config/aegis/config.yaml"

# ─── Show plan ─────────────────────────────────────────────────────────────────
echo ""
divider
header "Aegis Build Script — Linux"
divider
echo ""
echo "  The following actions are available:"
echo ""

# Action 1
BIN_STATUS=$( [ "${BIN_EXISTS}" = true ] && echo "(replaces existing binary)" || echo "(new install)" )
item "[1] Build aegis ${VERSION} and install binary"
detail "From : ./cmd/aegis"
detail "To   : ${BIN_DEST}  ${BIN_STATUS}"
detail "Go   : ${GO_VER}"
[ "${USE_SUDO}" = true ] && detail "Note : requires sudo to write to ${SYSTEM_BIN}"
[ -n "${EXISTING_BIN}" ] && detail "Old  : ${EXISTING_BIN}  (will be removed)"
echo ""

# Action 2
item "[2] Add aegis-config function to shell config"
if [ "${ALIAS_EXISTS}" = true ]; then
    detail "Status : aegis-config already present in ${ALIAS_FILE} — will skip"
else
    ALIAS_FILE_STATUS=$( [ -f "${ALIAS_FILE}" ] && echo "exists" || echo "will be created" )
    detail "Shell  : ${SHELL_NAME}"
    detail "File   : ${ALIAS_FILE}  (${ALIAS_FILE_STATUS})"
    detail "Config : ${AEGIS_CONFIG_PATH}"
    detail "Usage  : aegis-config  →  opens config in \$EDITOR / vi"
fi

echo ""
divider
echo ""

# ─── Prompt ────────────────────────────────────────────────────────────────────
printf "  Run which actions? [all / 1 2 / none]  (default: all): "
read -r SELECTION || SELECTION="all"
SELECTION="${SELECTION:-all}"
SELECTION=$(echo "${SELECTION}" | tr '[:upper:]' '[:lower:]' | xargs 2>/dev/null || echo "${SELECTION}")

RUN_BUILD=false
RUN_ALIAS=false

if [ "${SELECTION}" = "all" ]; then
    RUN_BUILD=true; RUN_ALIAS=true
elif [ "${SELECTION}" = "none" ]; then
    echo "  Nothing to do."; exit 0
else
    for num in ${SELECTION}; do
        case "${num}" in
            1) RUN_BUILD=true ;;
            2) RUN_ALIAS=true ;;
        esac
    done
fi

echo ""

# ─── Action 1 : Build ──────────────────────────────────────────────────────────
if [ "${RUN_BUILD}" = true ]; then
    header "[1] Building aegis ${VERSION}..."

    LDFLAGS="-s -w -X github.com/scottymacleod/aegis/internal/cli.Version=${VERSION}"
    go build -ldflags "${LDFLAGS}" -o ./aegis ./cmd/aegis

    # Remove any stale binary found at a different PATH location.
    if [ -n "${EXISTING_BIN}" ]; then
        detail "Removing old binary: ${EXISTING_BIN}"
        if [ -w "${EXISTING_BIN}" ]; then
            rm -f "${EXISTING_BIN}"
            ok "Removed:   ${EXISTING_BIN}"
        elif command -v sudo &>/dev/null; then
            if sudo rm -f "${EXISTING_BIN}"; then
                ok "Removed:   ${EXISTING_BIN}"
            else
                warn "Could not remove ${EXISTING_BIN} — continuing anyway"
            fi
        else
            warn "Could not remove ${EXISTING_BIN} (no permission, no sudo) — continuing anyway"
        fi
    fi

    mkdir -p "${INSTALL_DIR}"
    if [ "${USE_SUDO}" = true ]; then
        # This will prompt for a password if sudo requires one.
        if ! sudo install -m 755 ./aegis "${BIN_DEST}"; then
            warn "sudo install failed — falling back to ${USER_BIN}"
            INSTALL_DIR="${USER_BIN}"
            BIN_DEST="${INSTALL_DIR}/aegis"
            mkdir -p "${INSTALL_DIR}"
            install -m 755 ./aegis "${BIN_DEST}"
        fi
    else
        install -m 755 ./aegis "${BIN_DEST}"
    fi
    rm -f ./aegis

    ok "Installed: ${BIN_DEST}  (${VERSION})"

    # PATH check
    if ! echo "${PATH}" | tr ':' '\n' | grep -qx "${INSTALL_DIR}"; then
        warn "${INSTALL_DIR} is not in your PATH."
        if [ "${INSTALL_DIR}" = "${USER_BIN}" ]; then
            detail "Add to ~/.bashrc or ~/.profile:"
            detail "  export PATH=\"\${HOME}/go/bin:\${PATH}\""
        fi
    fi
    echo ""
fi

# ─── Action 2 : aegis-config ───────────────────────────────────────────────────
if [ "${RUN_ALIAS}" = true ]; then
    if [ "${ALIAS_EXISTS}" = true ]; then
        skip "[2] aegis-config already defined — nothing to do."
    else
        header "[2] Adding aegis-config to ${ALIAS_FILE}..."

        if [ "${ALIAS_METHOD}" = "fish" ]; then
            # Fish uses per-function files rather than a sourced aliases file.
            mkdir -p "$(dirname "${ALIAS_FILE}")"
            cat > "${ALIAS_FILE}" <<'FISHEOF'
# aegis-config: open the Aegis global configuration file in your editor.
# Run 'aegis --first-init' first if the file does not yet exist.
function aegis-config --description 'Open the Aegis configuration file'
    set cfg "$HOME/.config/aegis/config.yaml"
    if not test -f $cfg
        echo "Config not found at $cfg — run: aegis --first-init" >&2
        return 1
    end
    if set -q EDITOR
        $EDITOR $cfg
    else
        vi $cfg
    end
end
FISHEOF
            ok "Created: ${ALIAS_FILE}"
            detail "Reload: source ${ALIAS_FILE}  (or restart fish)"
        else
            # bash / zsh / sh — append a shell function.
            cat >> "${ALIAS_FILE}" <<SHEOF


# ── aegis-config ────────────────────────────────────────────────────────────────
# Opens the Aegis global configuration file in your preferred editor.
# Run 'aegis --first-init' first if the file does not yet exist.
aegis-config() {
    local cfg="\${HOME}/.config/aegis/config.yaml"
    if [ ! -f "\$cfg" ]; then
        echo "Config not found at \$cfg — run: aegis --first-init" >&2
        return 1
    fi
    "\${EDITOR:-vi}" "\$cfg"
}
SHEOF
            ok "Added to: ${ALIAS_FILE}"
            detail "Reload: source ${ALIAS_FILE}"
        fi
    fi
    echo ""
fi

# ─── Done ──────────────────────────────────────────────────────────────────────
divider
echo ""
ok "All done!"
echo ""
echo "  Next steps:"
detail "aegis --first-init        generate global config (first run only)"
detail "export OPENAI_API_KEY=ollama  required for Ollama (see config for others)"
detail "aegis                     start the TUI"
if [ "${RUN_ALIAS}" = true ] && [ "${ALIAS_EXISTS}" = false ]; then
    detail "aegis-config              open the config file  (after reloading shell)"
fi
echo ""
