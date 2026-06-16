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

info()    { echo -e "${CYAN}[INFO]${RESET}  $*" >&2; }
success() { echo -e "${GREEN}[OK]${RESET}    $*" >&2; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*" >&2; }
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

# ── Distribution helpers ─────────────────────────────────────────────────────
detect_distro_id() {
    if [[ -r /etc/os-release ]]; then
        . /etc/os-release
        echo "${ID:-}"
    else
        echo ""
    fi
}

detect_distro_like() {
    if [[ -r /etc/os-release ]]; then
        . /etc/os-release
        echo "${ID_LIKE:-}"
    else
        echo ""
    fi
}

pkg_manager() {
    if command -v apt-get &>/dev/null; then
        echo "apt"
    elif command -v dnf &>/dev/null; then
        echo "dnf"
    elif command -v pacman &>/dev/null; then
        echo "pacman"
    elif command -v zypper &>/dev/null; then
        echo "zypper"
    else
        echo ""
    fi
}

# ── Dependency checks (Wails desktop runtime/libs) ──────────────────────────
# Dynamically detect available packages using pattern matching.
# Exact versions of GTK/WebKit vary across distro releases, so we search
# for the best available package instead of hardcoding version suffixes.

# Patterns per package manager — matched against available/installed packages.
# Order matters: preferred/newer packages come first.
# apt (Debian/Ubuntu): version suffixes vary per release (4.0-37, 4.1-0, etc.)
dep_patterns_apt=("libgtk-3-0" "libgtk-3-dev" "libwebkit2gtk-4.1-*" "libwebkit2gtk-4.0-*" "libwebkit2gtk-4.1-dev" "libwebkit2gtk-4.0-dev")
# dnf (Fedora): uses webkit2gtk4.0 / webkit2gtk4.1 naming
dep_patterns_dnf=("gtk3" "webkit2gtk4.1" "webkit2gtk4.0")
# pacman (Arch Linux): webkit2gtk-4.1 is the current official package
dep_patterns_pacman=("gtk3" "webkit2gtk-4.1" "webkit2gtk")
# zypper (openSUSE): uses underscores in version (4_1-0, 4_0-37)
dep_patterns_zypper=("gtk3" "libwebkit2gtk-4_1-0" "libwebkit2gtk-4_0-*" "webkit2gtk3")

package_installed() {
    local pm="$1"
    local pkg="$2"
    case "${pm}" in
        apt) dpkg -s "${pkg}" &>/dev/null ;;
        dnf) rpm -q "${pkg}" &>/dev/null ;;
        pacman) pacman -Q "${pkg}" &>/dev/null ;;
        zypper) rpm -q "${pkg}" &>/dev/null ;;
        *) return 1 ;;
    esac
}

find_available_package() {
    local pm="$1"
    local pattern="$2"
    local contains_wildcard="${pattern%\*}"
    contains_wildcard="${contains_wildcard%\*}"
    local is_globby="false"
    [[ "${pattern}" == *"*"* ]] && is_globby="true"

    case "${pm}" in
        apt)
            # If exact name is installed, return as-is
            dpkg -s "${pattern}" &>/dev/null 2>&1 && { echo "${pattern}"; return; }
            # Search apt cache — for glob patterns use --names-only with the pattern,
            # for exact names anchor with ^...$
            local search_pat
            if [[ "${is_globby}" == "true" ]]; then
                search_pat="${pattern}"
            else
                search_pat="^${pattern}$"
            fi
            local candidate
            candidate="$(apt-cache search --names-only "${search_pat}" 2>/dev/null \
                | awk '{print $1}' | head -n1)"
            if [[ -n "${candidate}" ]]; then
                echo "${candidate}"
            fi
            ;;
        dnf)
            # Check if already installed (exact or glob match via rpm)
            if rpm -q "${pattern}" &>/dev/null 2>&1; then
                echo "${pattern}"
                return
            fi
            if command -v dnf &>/dev/null; then
                # dnf repoquery supports glob natively
                local candidate
                candidate="$(dnf repoquery --qf '%{name}' --latest-limit=1 "${pattern}" 2>/dev/null | head -n1)"
                if [[ -n "${candidate}" ]]; then
                    echo "${candidate}"
                fi
            fi
            ;;
        zypper)
            # Check if already installed
            if rpm -q "${pattern}" &>/dev/null 2>&1; then
                echo "${pattern}"
                return
            fi
            if command -v zypper &>/dev/null; then
                # zypper search -x uses exact match; for globs use -x with the pattern
                # The output format is: S | Name | Summary
                # We need to extract the Name column
                local candidate
                candidate="$(zypper --non-interactive search -x "${pattern}" 2>/dev/null \
                    | awk -F'|' 'NR>2{gsub(/^ +| +$/,"",$2); if($2!="") {print $2; exit}}')"
                if [[ -n "${candidate}" ]]; then
                    echo "${candidate}"
                fi
            fi
            ;;
        pacman)
            # Check if already installed
            if pacman -Q "${pattern}" &>/dev/null 2>&1; then
                echo "${pattern}"
                return
            fi
            # pacman -Ss searches sync db; use regex for exact or glob match
            local search_re
            if [[ "${is_globby}" == "true" ]]; then
                # Convert shell glob to regex: * -> .*
                search_re="^$(echo "${pattern}" | sed 's/[.]/\\./g; s/\*/.*/g')$"
            else
                search_re="^${pattern}$"
            fi
            if pacman -Ss "${search_re}" &>/dev/null 2>&1; then
                # Extract package name (format: repo/name version ...)
                local found_name
                found_name="$(pacman -Ss "${search_re}" 2>/dev/null | head -n1 | awk '{print $1}' | awk -F'/' '{print $2}')"
                if [[ -n "${found_name}" ]]; then
                    echo "${found_name}"
                fi
            fi
            ;;
    esac
}

build_runtime_dependency_list() {
    local pm="$1"
    local -n patterns="dep_patterns_${pm}"
    local deps=()
    local seen=()

    if [[ ${#patterns[@]} -eq 0 ]]; then
        return
    fi

    info "Searching for available desktop runtime packages..."

    for pattern in "${patterns[@]}"; do
        # Skip if we already found a package for this logical dependency
        # (e.g., once we find webkit2gtk-4.1, skip webkit2gtk)
        local already_found=false
        for s in "${seen[@]}"; do
            case "${pm}" in
                apt)
                    # For apt: libwebkit2gtk-4.1-* and libwebkit2gtk-4.0-* are the same logical dep
                    if [[ "${s}" == "webkit" && "${pattern}" == libwebkit2gtk-* ]]; then
                        already_found=true; break
                    fi
                    ;;
                dnf)
                    if [[ "${s}" == "webkit" && "${pattern}" == webkit2gtk* ]]; then
                        already_found=true; break
                    fi
                    ;;
                pacman)
                    if [[ "${s}" == "webkit" && "${pattern}" == webkit2gtk* ]]; then
                        already_found=true; break
                    fi
                    ;;
                zypper)
                    if [[ "${s}" == "webkit" && "${pattern}" == *webkit* ]]; then
                        already_found=true; break
                    fi
                    ;;
            esac
        done
        [[ "${already_found}" == "true" ]] && continue

        local found
        found="$(find_available_package "${pm}" "${pattern}")"
        if [[ -n "${found}" ]]; then
            deps+=("${found}")
            success "Found: ${found}"
            # Mark logical category as seen
            if [[ "${pattern}" == *gtk* ]]; then
                seen+=("gtk")
            elif [[ "${pattern}" == *webkit* ]]; then
                seen+=("webkit")
            fi
        fi
    done

    printf '%s\n' "${deps[@]}"
}

run_sudo_install_cmd() {
    local pm="$1"
    shift
    local missing=("$@")

    if [[ ${#missing[@]} -eq 0 ]]; then
        return 0
    fi

    if ! command -v sudo &>/dev/null; then
        error "sudo is required to install missing system packages automatically. Install sudo and re-run."
    fi

    info "Requesting sudo access to install missing desktop runtime libraries..."
    sudo -v

    case "${pm}" in
        apt)
            sudo apt-get update
            sudo apt-get install -y "${missing[@]}"
            ;;
        dnf)
            sudo dnf install -y "${missing[@]}"
            ;;
        pacman)
            sudo pacman -Sy --needed --noconfirm "${missing[@]}"
            ;;
        zypper)
            sudo zypper --non-interactive install --auto-agree-with-licenses "${missing[@]}"
            ;;
        *)
            error "Unsupported package manager for automatic installation: ${pm}"
            ;;
    esac
}

ensure_wails_runtime_dependencies() {
    local pm
    pm="$(pkg_manager)"

    if [[ -z "${pm}" ]]; then
        warn "Could not detect a supported package manager (apt/dnf/pacman/zypper)."
        warn "Skipping automatic dependency installation for Wails desktop runtime."
        warn "Please ensure your system has GTK and WebKitGTK runtime libraries installed."
        return 0
    fi

    local deps=()
    local missing=()

    while IFS= read -r dep; do
        [[ -n "${dep}" ]] && deps+=("${dep}")
    done < <(build_runtime_dependency_list "${pm}")

    if [[ ${#deps[@]} -eq 0 ]]; then
        warn "No runtime dependency packages found for package manager '${pm}'."
        warn "Skipping automatic dependency installation."
        warn "Please ensure your system has GTK and WebKitGTK runtime libraries installed."
        return 0
    fi

    info "Checking Linux desktop runtime dependencies (Wails) for ${pm}..."

    for dep in "${deps[@]}"; do
        if package_installed "${pm}" "${dep}"; then
            success "Dependency present: ${dep}"
        else
            warn "Missing dependency: ${dep}"
            missing+=("${dep}")
        fi
    done

    if [[ ${#missing[@]} -eq 0 ]]; then
        success "All required desktop runtime dependencies are installed."
        return 0
    fi

    warn "Installing missing dependencies with sudo: ${missing[*]}"
    run_sudo_install_cmd "${pm}" "${missing[@]}"

    local still_missing=()
    for dep in "${missing[@]}"; do
        if ! package_installed "${pm}" "${dep}"; then
            still_missing+=("${dep}")
        fi
    done

    if [[ ${#still_missing[@]} -gt 0 ]]; then
        error "Some dependencies are still missing after installation: ${still_missing[*]}"
    fi

    success "Desktop runtime dependencies installed successfully."
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

    local distro_id distro_like
    distro_id="$(detect_distro_id)"
    distro_like="$(detect_distro_like)"
    if [[ -n "${distro_id}" ]]; then
        info "Detected distro: ${distro_id}${distro_like:+ (like: ${distro_like})}"
    fi

    ensure_wails_runtime_dependencies

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

    # Find the release binary (archive contains arch-specific name)
    local expected_bin="pando-linux-${arch}"
    local pando_bin
    pando_bin="$(find "${tmp_dir}/extracted" -type f -name "${expected_bin}" | head -n1)"
    if [[ -z "${pando_bin}" ]]; then
        # Fallback for possible packaging layout/name variations
        pando_bin="$(find "${tmp_dir}/extracted" -type f -name "pando*" | head -n1)"
    fi
    [[ -z "${pando_bin}" ]] && error "Could not find Pando binary inside the downloaded archive."

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
