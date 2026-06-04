#!/usr/bin/env bash
# install-linux.sh — Pando Linux installer
# Downloads the latest release from GitHub and installs it in the user's bin path.

set -euo pipefail

GITHUB_REPO="digiogithub/pando"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
ICON_URL="https://raw.githubusercontent.com/${GITHUB_REPO}/main/assets/pando_icon0_optim.png"
INSTALL_DIR="${HOME}/.local/bin"
ICON_DIR="${HOME}/.local/share/icons/hicolor/256x256/apps"
DESKTOP_DIR="${HOME}/.local/share/applications"

# ── Colors ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

info()    { echo -e "${CYAN}[INFO]${RESET}  $*"; }
success() { echo -e "${GREEN}[OK]${RESET}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*"; }
error()   { echo -e "${RED}[ERROR]${RESET} $*" >&2; exit 1; }

# ── Detect architecture ──────────────────────────────────────────────────────
detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64|amd64)   echo "x64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              error "Unsupported architecture: ${machine}" ;;
    esac
}

# ── Get latest release version from GitHub ──────────────────────────────────
get_latest_version() {
    if command -v curl &>/dev/null; then
        curl -fsSL "${GITHUB_API}" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/'
    elif command -v wget &>/dev/null; then
        wget -qO- "${GITHUB_API}" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/'
    else
        error "Neither curl nor wget is available. Please install one of them."
    fi
}

# ── Get currently installed version ─────────────────────────────────────────
get_installed_version() {
    if command -v pando &>/dev/null; then
        pando --version 2>/dev/null | sed 's/+dirty//' | tr -d '[:space:]'
    else
        echo ""
    fi
}

# ── Download helper ──────────────────────────────────────────────────────────
download() {
    local url="$1"
    local dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL --progress-bar -o "${dest}" "${url}"
    elif command -v wget &>/dev/null; then
        wget --show-progress -qO "${dest}" "${url}"
    else
        error "Neither curl nor wget is available."
    fi
}

# ── Ensure required tools ────────────────────────────────────────────────────
require_tool() {
    command -v "$1" &>/dev/null || error "Required tool not found: $1. Please install it and re-run."
}

require_tool unzip

# ── Main ─────────────────────────────────────────────────────────────────────
main() {
    echo -e "${BOLD}Pando Linux Installer${RESET}"
    echo "────────────────────────────────────────"

    local arch
    arch="$(detect_arch)"
    info "Architecture detected: ${arch}"

    local latest_version
    info "Fetching latest release information..."
    latest_version="$(get_latest_version)"
    [[ -z "${latest_version}" ]] && error "Could not determine the latest release version."
    info "Latest version available: ${latest_version}"

    # Check currently installed version
    local installed_version
    installed_version="$(get_installed_version)"
    if [[ -n "${installed_version}" ]]; then
        info "Currently installed version: ${installed_version}"
        # Normalise: strip leading 'v' for comparison
        local cmp_latest="${latest_version#v}"
        local cmp_installed="${installed_version#v}"
        if [[ "${cmp_installed}" == "${cmp_latest}" ]]; then
            success "Pando ${installed_version} is already up to date."
            exit 0
        else
            warn "Updating from ${installed_version} → ${latest_version}"
        fi
    else
        info "No existing Pando installation found. Proceeding with fresh install."
    fi

    # Build download URL
    local zip_name="pando-linux-${arch}.zip"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${latest_version}/${zip_name}"

    # Create temp dir
    local tmp_dir
    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "${tmp_dir}"' EXIT

    info "Downloading ${zip_name} from ${download_url} ..."
    download "${download_url}" "${tmp_dir}/${zip_name}"

    info "Extracting archive..."
    unzip -q "${tmp_dir}/${zip_name}" -d "${tmp_dir}/extracted"

    # Find the pando binary (may be at root or inside a subdirectory)
    local pando_bin
    pando_bin="$(find "${tmp_dir}/extracted" -type f -name "pando*" | head -n1)"
    [[ -z "${pando_bin}" ]] && error "Could not find 'pando' binary inside the downloaded archive."

    # Install binary
    mkdir -p "${INSTALL_DIR}"
    install -m 755 "${pando_bin}" "${INSTALL_DIR}/pando"
    success "Binary installed to ${INSTALL_DIR}/pando"

    # Ensure INSTALL_DIR is in PATH (add to shell config if missing)
    if ! echo "${PATH}" | tr ':' '\n' | grep -qx "${INSTALL_DIR}"; then
        warn "${INSTALL_DIR} is not in your PATH."
        for rc in "${HOME}/.bashrc" "${HOME}/.zshrc" "${HOME}/.profile"; do
            if [[ -f "${rc}" ]]; then
                if ! grep -q "${INSTALL_DIR}" "${rc}"; then
                    echo -e "\nexport PATH=\"${INSTALL_DIR}:\$PATH\"" >> "${rc}"
                    info "Added ${INSTALL_DIR} to PATH in ${rc}"
                fi
            fi
        done
        warn "Restart your terminal or run: export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi

    # Download icon
    mkdir -p "${ICON_DIR}"
    info "Downloading application icon..."
    download "${ICON_URL}" "${ICON_DIR}/pando.png"
    success "Icon saved to ${ICON_DIR}/pando.png"

    # Create .desktop entry
    mkdir -p "${DESKTOP_DIR}"
    cat > "${DESKTOP_DIR}/pando.desktop" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=Pando
Comment=AI assistant for software developers
Exec=${INSTALL_DIR}/pando desktop
Icon=${ICON_DIR}/pando.png
Terminal=false
Categories=Development;Utility;
Keywords=ai;assistant;developer;
StartupNotify=true
EOF
    chmod 644 "${DESKTOP_DIR}/pando.desktop"
    success ".desktop entry created at ${DESKTOP_DIR}/pando.desktop"

    # Update desktop database if available
    if command -v update-desktop-database &>/dev/null; then
        update-desktop-database "${DESKTOP_DIR}" 2>/dev/null || true
    fi

    echo "────────────────────────────────────────"
    if [[ -n "${installed_version}" ]]; then
        success "Pando updated: ${installed_version} → ${latest_version}"
    else
        success "Pando ${latest_version} installed successfully!"
    fi
    echo -e "${BOLD}Run:${RESET} pando --help"
}

main "$@"
