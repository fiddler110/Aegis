#!/usr/bin/env bash
# build-macos.sh — Build Aegis and set up your shell for first-time use on macOS.
#
# Four actions, [3] and [4] are opt-in and not part of "all":
#   [1] Compile aegis and install it to /usr/local/bin (or ~/go/bin fallback)
#   [2] Add an aegis-config function to your shell's aliases file so you can
#       run "aegis-config" to open the Aegis config file in your editor
#   [3] (opt-in, RECOMMENDED) Build the container scanner images and fill the
#       vulnerability-database cache volume, so `aegis scan` / the security_scan
#       tool run pinned, verified, confined scanners instead of whatever host
#       binaries happen to be installed
#   [4] (opt-in, fallback) Install the host security scanner binaries one at a
#       time via the guided `aegis security install` command — for machines with
#       no container runtime, plus dockle, which no scanner image can carry
#
# [3] and [4] are alternatives, not a sequence. Prefer [3]: host binaries are
# unpinned and unconfined, so two machines silently scan with different rule
# sets, and under `default_method: auto` Aegis now prefers the container anyway.
#
# Alias file priority (first existing file wins; file is created if none exist):
#   zsh  : ~/.zsh_aliases  ~/.zshrc_aliases  ~/.aliases  → ~/.zshrc  (macOS default)
#   bash : ~/.bash_aliases  ~/.aliases                   → ~/.bash_profile
#   fish : ~/.config/fish/functions/aegis-config.fish    (function file)
#   other: ~/.aliases                                    → ~/.profile
#
# Usage:
#   chmod +x build-macos.sh && ./build-macos.sh
#   ./build-macos.sh 1        # build only
#   ./build-macos.sh 2        # shell config only
#   ./build-macos.sh 3        # container scanner images only (opt-in)
#   ./build-macos.sh 4        # host scanner binaries only (opt-in)
#   ./build-macos.sh all      # actions 1 + 2 (never includes 3/4 — opt-in)
#   ./build-macos.sh "all 3"  # actions 1 + 2 + 3 (recommended)

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

# Every entry in SECURITY_TOOLS has a binary name equal to its scanner name
# (verified against internal/security/method.go), so a plain PATH lookup is
# enough to tell whether a tool is already installed before re-running its
# installer.
tool_installed() { command -v "$1" &>/dev/null; }

# ─── Locate Go ─────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
    echo "Error: Go is not installed or not in PATH." >&2
    echo "Install from : https://go.dev/dl/" >&2
    echo "Via Homebrew : brew install go" >&2
    exit 1
fi
GO_VER=$(go version)

# ─── Resolve binary install location ───────────────────────────────────────────
# /usr/local/bin is valid on both Intel and Apple Silicon and is in PATH via
# /etc/paths. On Apple Silicon, /opt/homebrew/bin is Homebrew's home and not
# the right place for user-compiled binaries.
SYSTEM_BIN="/usr/local/bin"
# Use go env GOPATH so we respect a non-default GOPATH instead of assuming ~/go.
USER_BIN="$(go env GOPATH 2>/dev/null || echo "${HOME}/go")/bin"
INSTALL_DIR=""
USE_SUDO=false
BIN_EXISTS=false

# Ensure /usr/local/bin exists (macOS creates it, but guard anyway).
if [ ! -d "${SYSTEM_BIN}" ]; then
    mkdir -p "${SYSTEM_BIN}" 2>/dev/null || sudo mkdir -p "${SYSTEM_BIN}"
fi

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
# macOS ships zsh as the default shell since Catalina.
SHELL_NAME=$(basename "${SHELL:-/bin/zsh}")
ALIAS_FILE=""
ALIAS_METHOD="append"   # "append" | "fish"

case "${SHELL_NAME}" in
    zsh)
        for f in "${HOME}/.zsh_aliases" "${HOME}/.zshrc_aliases" "${HOME}/.aliases"; do
            if [ -f "$f" ]; then ALIAS_FILE="$f"; break; fi
        done
        # macOS zsh default: ~/.zshrc (not ~/.zprofile, which is login-only)
        [ -z "${ALIAS_FILE}" ] && ALIAS_FILE="${HOME}/.zshrc"
        ;;
    bash)
        for f in "${HOME}/.bash_aliases" "${HOME}/.aliases"; do
            if [ -f "$f" ]; then ALIAS_FILE="$f"; break; fi
        done
        # macOS bash: .bash_profile is sourced for login shells; .bashrc rarely is.
        [ -z "${ALIAS_FILE}" ] && ALIAS_FILE="${HOME}/.bash_profile"
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

# Check whether the function is already defined.
ALIAS_EXISTS=false
if [ "${ALIAS_METHOD}" = "fish" ]; then
    [ -f "${ALIAS_FILE}" ] && ALIAS_EXISTS=true
elif [ -f "${ALIAS_FILE}" ] && grep -q 'aegis-config' "${ALIAS_FILE}" 2>/dev/null; then
    ALIAS_EXISTS=true
fi

AEGIS_CONFIG_PATH="${HOME}/.config/aegis/config.yaml"

# ─── Container scanner images (opt-in action [3]) ──────────────────────────────
# Only docker and podman can BUILD the images: the build shells out to
# "<runtime> build -f Containerfile". Apple Containers is a supported *run*
# backend for the sandbox but is not a build engine here, and Aegis records which
# engine built an image because a locally-built image exists solely in that
# engine's storage. On macOS both are VM-backed (Docker Desktop / Colima /
# `podman machine`), so "installed" and "running" are genuinely different states.
CONTAINER_RUNTIME=""
for _rt in docker podman; do
    if command -v "${_rt}" &>/dev/null && "${_rt}" info &>/dev/null; then
        CONTAINER_RUNTIME="${_rt}"
        break
    fi
done
CONTAINER_RUNTIME_PRESENT_BUT_DOWN=""
if [ -z "${CONTAINER_RUNTIME}" ]; then
    for _rt in docker podman; do
        if command -v "${_rt}" &>/dev/null; then
            CONTAINER_RUNTIME_PRESENT_BUT_DOWN="${_rt}"
            break
        fi
    done
fi

# ─── Security scanner tools (opt-in action [4]) ────────────────────────────────
# Mirrors internal/security/method.go's descriptor list — every scanner that has
# a guided host install. zap has none (it keeps its own official image, invoked
# directly), so it is not here.
#
# Ordered so the tools a container image can never carry come first: dockle needs
# the container engine socket, which is effectively host root and which neither
# scanner image grants. Everything after it is also available via action [3],
# pinned and confined — install these on the host only if you have no container
# runtime, or you specifically want a host binary for one tool.
SECURITY_TOOLS_HOST_ONLY=(dockle)
SECURITY_TOOLS_ALSO_CONTAINERIZED=(opengrep gosec bandit brakeman njsscan trivy gitleaks trufflehog kubescape hadolint grype osv-scanner syft nmap nuclei)
SECURITY_TOOLS=("${SECURITY_TOOLS_HOST_ONLY[@]}" "${SECURITY_TOOLS_ALSO_CONTAINERIZED[@]}")

# ─── Show plan ─────────────────────────────────────────────────────────────────
echo ""
divider
header "Aegis Build Script — macOS  ($(uname -m))"
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
    detail "Usage  : aegis-config  →  opens config in your preferred editor (prompted)"
fi

echo ""

# Action 3 (opt-in — never implied by "all")
item "[3] (opt-in, recommended) Build the container scanner images"
detail "Runs   : aegis security build-image  (+ update-db), using the binary from [1]"
detail "Asks   : which profile — core (static scanners) or full (+ Python/Ruby/Go/"
detail "         network), and whether to also build the netscanner image"
if [ -n "${CONTAINER_RUNTIME}" ]; then
    detail "Runtime: ${CONTAINER_RUNTIME}  (detected — the image is pinned to the engine that builds it)"
elif [ -n "${CONTAINER_RUNTIME_PRESENT_BUT_DOWN}" ]; then
    detail "Runtime: ${CONTAINER_RUNTIME_PRESENT_BUT_DOWN} found but not responding — start its VM first"
    detail "         (open Docker Desktop / 'colima start' / 'podman machine start')"
else
    detail "Runtime: NONE FOUND — install Docker Desktop, Colima or Podman, or use [4]"
fi
detail "Note   : the build is long and large (core ~1GB, full ~2-3GB, netscanner"
detail "         ~570MB). Vulnerability databases are NOT in the image — update-db"
detail "         fills a shared volume so scans don't re-download ~1.2GB each time."
detail "         The pin is written machine-wide to your user config, not this repo."

echo ""

# Action 4 (opt-in — never implied by "all")
item "[4] (opt-in, fallback) Install host security scanner binaries"
detail "Tools  : ${SECURITY_TOOLS[*]}"
detail "Runs   : aegis security install <tool> --yes  for each, using the binary from [1]"
detail "Skips  : any tool already found on PATH is left alone, not reinstalled"
detail "Note   : prefer [3]. Host binaries are unpinned and unconfined, so two machines"
detail "         can silently scan with different rule sets, and under default_method:"
detail "         auto a tool the image carries resolves to the container regardless."
detail "         dockle is the exception — it needs the container engine socket, so no"
detail "         scanner image can carry it and a host install is the only option."
detail "         Best-effort: many are language-specific (Go/pipx/gem) and may fail"
detail "         individually; failures are reported but don't stop the run."

echo ""
divider
echo ""

# ─── Prompt (or use supplied argument) ────────────────────────────────────────
if [ -n "${1:-}" ]; then
    SELECTION=$(echo "${1}" | tr '[:upper:]' '[:lower:]' | xargs 2>/dev/null || echo "${1}")
else
    printf "  Run which actions? [all / 1 2 3 4 / none]  (default: all — 3 and 4 are opt-in): "
    read -r SELECTION || SELECTION="all"
    SELECTION="${SELECTION:-all}"
    SELECTION=$(echo "${SELECTION}" | tr '[:upper:]' '[:lower:]' | xargs 2>/dev/null || echo "${SELECTION}")
fi

RUN_BUILD=false
RUN_ALIAS=false
RUN_IMAGES=false
RUN_TOOLS=false

if [ "${SELECTION}" = "none" ]; then
    echo "  Nothing to do."; exit 0
fi

for token in ${SELECTION}; do
    case "${token}" in
        all) RUN_BUILD=true; RUN_ALIAS=true ;;
        1) RUN_BUILD=true ;;
        2) RUN_ALIAS=true ;;
        3) RUN_IMAGES=true ;;
        4) RUN_TOOLS=true ;;
    esac
done

echo ""

# ─── Action 1 : Build ──────────────────────────────────────────────────────────
if [ "${RUN_BUILD}" = true ]; then
    header "[1] Building aegis ${VERSION}..."

    LDFLAGS="-s -w -X github.com/fiddler110/aegis/internal/cli.Version=${VERSION}"
    if ! go build -ldflags "${LDFLAGS}" -o ./aegis ./cmd/aegis; then
        echo "Build failed." >&2; exit 1
    fi

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
            detail "Add to ~/.zshrc or ~/.bash_profile:"
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
        echo ""

        # ── Editor selection ───────────────────────────────────────────────────
        echo "    Choose your preferred editor for aegis-config:"
        echo ""
        _EDITORS=()
        _IDX=1
        if command -v code &>/dev/null; then
            detail "[${_IDX}] code  — Visual Studio Code"
            _EDITORS+=("code"); _IDX=$(( _IDX + 1 ))
        fi
        if command -v nano &>/dev/null; then
            detail "[${_IDX}] nano  — nano"
            _EDITORS+=("nano"); _IDX=$(( _IDX + 1 ))
        fi
        detail "[${_IDX}] vi    — vi / vim"
        _EDITORS+=("vi"); _IDX=$(( _IDX + 1 ))
        detail "[${_IDX}] \$EDITOR  — use \$EDITOR env var  (currently: ${EDITOR:-not set})"
        _EDITORS+=("dynamic")
        _TOTAL=${_IDX}
        echo ""
        printf "        Select [1-%d]  (default: 1): " "${_TOTAL}"
        read -r _SEL || _SEL="1"
        _SEL="${_SEL:-1}"
        if ! echo "${_SEL}" | grep -qE '^[0-9]+$' || \
            [ "${_SEL}" -lt 1 ] || [ "${_SEL}" -gt "${_TOTAL}" ]; then
            warn "Invalid selection — using option 1"; _SEL=1
        fi
        _CHOSEN="${_EDITORS[$(( _SEL - 1 ))]}"

        _EDITOR_FIXED=false
        if [ "${_CHOSEN}" != "dynamic" ]; then
            echo ""
            printf "        Always use '%s' for aegis-config? [Y/n]: " "${_CHOSEN}"
            read -r _ALWAYS || _ALWAYS="y"
            _ALWAYS="${_ALWAYS:-y}"
            _ALWAYS=$(echo "${_ALWAYS}" | tr '[:upper:]' '[:lower:]')
            [ "${_ALWAYS}" != "n" ] && _EDITOR_FIXED=true
        fi
        echo ""

        if [ "${ALIAS_METHOD}" = "fish" ]; then
            mkdir -p "$(dirname "${ALIAS_FILE}")"
            if [ "${_EDITOR_FIXED}" = true ]; then
                cat > "${ALIAS_FILE}" <<FISHEOF
# aegis-config: open the Aegis global configuration file in your editor.
# Run 'aegis --first-init' first if the file does not yet exist.
function aegis-config --description 'Open the Aegis configuration file'
    set cfg "\$HOME/.config/aegis/config.yaml"
    if not test -f \$cfg
        echo "Config not found at \$cfg — run: aegis --first-init" >&2
        return 1
    end
    ${_CHOSEN} \$cfg
end
FISHEOF
            elif [ "${_CHOSEN}" = "dynamic" ]; then
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
            else
                cat > "${ALIAS_FILE}" <<FISHEOF
# aegis-config: open the Aegis global configuration file in your editor.
# Run 'aegis --first-init' first if the file does not yet exist.
function aegis-config --description 'Open the Aegis configuration file'
    set cfg "\$HOME/.config/aegis/config.yaml"
    if not test -f \$cfg
        echo "Config not found at \$cfg — run: aegis --first-init" >&2
        return 1
    end
    if set -q EDITOR
        \$EDITOR \$cfg
    else
        ${_CHOSEN} \$cfg
    end
end
FISHEOF
            fi
            ok "Created: ${ALIAS_FILE}"
            detail "Reload: source ${ALIAS_FILE}  (or restart fish)"
        else
            if [ "${_EDITOR_FIXED}" = true ]; then
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
    ${_CHOSEN} "\$cfg"
}
SHEOF
            elif [ "${_CHOSEN}" = "dynamic" ]; then
                cat >> "${ALIAS_FILE}" <<'SHEOF'


# ── aegis-config ────────────────────────────────────────────────────────────────
# Opens the Aegis global configuration file in your preferred editor.
# Run 'aegis --first-init' first if the file does not yet exist.
aegis-config() {
    local cfg="${HOME}/.config/aegis/config.yaml"
    if [ ! -f "$cfg" ]; then
        echo "Config not found at $cfg — run: aegis --first-init" >&2
        return 1
    fi
    "${EDITOR:-vi}" "$cfg"
}
SHEOF
            else
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
    "\${EDITOR:-${_CHOSEN}}" "\$cfg"
}
SHEOF
            fi
            ok "Added to: ${ALIAS_FILE}"
            detail "Reload: source ${ALIAS_FILE}"
        fi
    fi
    echo ""
fi

# ─── Resolve the aegis binary for actions [3] and [4] ─────────────────────────
resolve_aegis_bin() {
    if [ "${RUN_BUILD}" = true ] && [ -x "${BIN_DEST}" ]; then
        echo "${BIN_DEST}"
    elif command -v aegis &>/dev/null; then
        command -v aegis
    fi
}

# ─── Action 3 : Build the container scanner images (opt-in) ───────────────────
if [ "${RUN_IMAGES}" = true ]; then
    header "[3] Building container scanner images..."
    echo ""

    AEGIS_BIN="$(resolve_aegis_bin)"
    if [ -z "${AEGIS_BIN}" ]; then
        warn "aegis binary not found — run action [1] first (or ensure aegis is on PATH), then re-run with 3."
    elif [ -z "${CONTAINER_RUNTIME}" ]; then
        if [ -n "${CONTAINER_RUNTIME_PRESENT_BUT_DOWN}" ]; then
            warn "${CONTAINER_RUNTIME_PRESENT_BUT_DOWN} is installed but not responding to '${CONTAINER_RUNTIME_PRESENT_BUT_DOWN} info'."
            detail "Start its VM and re-run with 3:  open Docker Desktop | colima start | podman machine start"
        else
            warn "No container runtime found — the images need docker or podman to build."
            detail "Install Docker Desktop, Colima or Podman, or use action [4] for host binaries."
        fi
    else
        detail "Using: ${AEGIS_BIN}"
        detail "Runtime: ${CONTAINER_RUNTIME}"
        echo ""

        # ── Profile selection ──────────────────────────────────────────────────
        echo "    Which multiscanner profile?"
        echo ""
        detail "[1] full  — every filesystem scanner: adds Python (bandit/njsscan), Ruby"
        detail "            (brakeman), Go (gosec + a pinned Go toolchain) and network"
        detail "            (nmap/nuclei) on top of core. ~2-3GB, long first build."
        detail "[2] core  — static binaries only: trivy, gitleaks, trufflehog, syft,"
        detail "            osv-scanner, grype, kubescape, hadolint, opengrep. ~1GB."
        echo ""
        printf "        Select [1-2]  (default: 1): "
        read -r _PSEL || _PSEL="1"
        _PSEL="${_PSEL:-1}"
        IMAGE_PROFILE="full"
        [ "${_PSEL}" = "2" ] && IMAGE_PROFILE="core"

        echo ""
        printf "        Also build the netscanner image (~570MB — nmap, nuclei, and\n"
        printf "        image-reference scanning with trivy/grype; runs with network ON\n"
        printf "        and your workspace never mounted)? [y/N]: "
        read -r _NSEL || _NSEL="n"
        _NSEL=$(echo "${_NSEL:-n}" | tr '[:upper:]' '[:lower:]')
        BUILD_NETSCANNER=false
        [ "${_NSEL}" = "y" ] && BUILD_NETSCANNER=true
        echo ""

        IMAGE_FAILURES=()

        # build-image verifies the result itself (each scanner is probed AND run
        # against a fixture with planted findings), and pins the image ID into
        # the user config. A --version probe alone can't catch a tool that exits
        # clean reporting zero because it never loaded its data, which is how two
        # scanners previously shipped broken — so we never pass --skip-verify.
        header "  build-image --profile ${IMAGE_PROFILE}"
        if "${AEGIS_BIN}" security build-image --profile "${IMAGE_PROFILE}" --runtime "${CONTAINER_RUNTIME}"; then
            ok "multiscanner image built, verified and pinned"
        else
            warn "multiscanner build failed — see output above"
            IMAGE_FAILURES+=("build-image")
        fi
        echo ""

        # Only worth attempting if the image exists: update-db runs a container
        # from it. This is the one container run given network access, and it
        # mounts no workspace.
        if [ "${#IMAGE_FAILURES[@]}" -eq 0 ]; then
            header "  update-db  (fills the shared vulnerability-database volume)"
            detail "Needs network. Scans run with --network none and read this volume, so"
            detail "without it the database-backed scanners are refused rather than silently"
            detail "reporting zero findings."
            if "${AEGIS_BIN}" security update-db; then
                ok "vulnerability databases populated"
            else
                warn "update-db failed — re-run 'aegis security update-db' once network is available"
                IMAGE_FAILURES+=("update-db")
            fi
            echo ""
        fi

        if [ "${BUILD_NETSCANNER}" = true ]; then
            header "  build-image --netscanner"
            detail "Its verification needs network — proving trivy/grype load a database"
            detail "against a known-vulnerable image is the whole point. No update-db needed."
            if "${AEGIS_BIN}" security build-image --netscanner --runtime "${CONTAINER_RUNTIME}"; then
                ok "netscanner image built, verified and pinned"
            else
                warn "netscanner build failed — see output above"
                IMAGE_FAILURES+=("build-image --netscanner")
            fi
            echo ""
        fi

        if [ "${#IMAGE_FAILURES[@]}" -gt 0 ]; then
            warn "Some image steps failed: ${IMAGE_FAILURES[*]}"
        else
            ok "Container scanners are ready."
        fi
        detail "Run 'aegis security status' to see which scanners resolve to the images,"
        detail "and how old each vulnerability database is."
    fi
    echo ""
fi

# ─── Action 4 : Install host security scanner binaries (opt-in) ───────────────
if [ "${RUN_TOOLS}" = true ]; then
    header "[4] Installing host security scanner binaries..."
    echo ""

    AEGIS_BIN="$(resolve_aegis_bin)"

    if [ -z "${AEGIS_BIN}" ]; then
        warn "aegis binary not found — run action [1] first (or ensure aegis is on PATH), then re-run with 4."
    else
        if [ -n "${CONTAINER_RUNTIME}" ] && [ "${RUN_IMAGES}" != true ]; then
            warn "${CONTAINER_RUNTIME} is available — action [3] would give you pinned, verified,"
            detail "confined scanners instead. Continuing with host installs as requested."
            echo ""
        fi
        detail "Using: ${AEGIS_BIN}"
        echo ""
        FAILED_TOOLS=()
        SKIPPED_TOOLS=()
        for tool in "${SECURITY_TOOLS[@]}"; do
            item "${tool}"
            if tool_installed "${tool}"; then
                skip "${tool} already installed — $(command -v "${tool}")"
                SKIPPED_TOOLS+=("${tool}")
                echo ""
                continue
            fi
            if "${AEGIS_BIN}" security install "${tool}" --yes; then
                ok "${tool} installed"
            else
                warn "${tool} failed — see output above"
                FAILED_TOOLS+=("${tool}")
            fi
            echo ""
        done
        if [ "${#SKIPPED_TOOLS[@]}" -gt 0 ]; then
            detail "Already installed (skipped): ${SKIPPED_TOOLS[*]}"
        fi
        if [ "${#FAILED_TOOLS[@]}" -gt 0 ]; then
            warn "Some tools failed to install: ${FAILED_TOOLS[*]}"
            detail "Often expected — several tools need a specific language toolchain (Go/pipx/gem)"
            detail "or Homebrew, and only matter for projects that use that language."
            detail "Action [3] avoids this entirely for every tool except dockle."
        else
            ok "All security tools are installed."
        fi
        detail "Run 'aegis security status' to confirm what's available."
    fi
    echo ""
fi

# ─── Done ──────────────────────────────────────────────────────────────────────
divider
echo ""
ok "All done!"
echo ""
echo "  Next steps:"
detail "aegis --first-init              generate global config (first run only)"
detail "aegis --init                    create .aegis/config.yaml project override (optional)"
detail "export OPENAI_API_KEY=ollama    required for Ollama; set ANTHROPIC_API_KEY for Claude"
detail "ollama pull <model>             pull at least one model  (e.g. ollama pull llama3.2)"
detail "                                  model: auto in config selects it automatically"
detail "                                  Aegis starts Ollama itself if it is installed"
detail "aegis                           start the TUI"
detail "aegis ui                        open the web UI in your browser"
detail "aegis security status           check which security scanners are available"
if [ "${RUN_ALIAS}" = true ] && [ "${ALIAS_EXISTS}" = false ]; then
    detail "aegis-config                    open the config file  (after reloading shell)"
fi
if [ "${RUN_IMAGES}" != true ]; then
    detail "./build-macos.sh 3              build the container scanner images (opt-in, not run yet)"
fi
if [ "${RUN_TOOLS}" != true ]; then
    detail "./build-macos.sh 4              install host scanner binaries (opt-in, not run yet)"
fi
if [ "${RUN_IMAGES}" = true ]; then
    detail "aegis security update-db        re-run periodically — a stale database"
    detail "                                  under-reports rather than failing"
fi
echo ""
