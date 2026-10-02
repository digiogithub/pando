#!/usr/bin/env bash
# install.sh — Pando installer for Linux and macOS
#
# Downloads a release from GitHub and installs it for the current user.
#
#   curl -fsSL https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install.sh | bash
#   curl -fsSL .../install.sh | bash -s -- --version v1.2.7 --no-desktop
#
# Linux : pando-linux-<arch>.zip -> ~/.local/bin/pando, plus a .desktop entry
#         and the GTK/WebKitGTK runtime the desktop window needs.
# macOS : pando-<version>-darwin-<arch>.pkg (signed and notarized), which
#         installs /Applications/Pando.app and /usr/local/bin/pando.
#         With --cli-only: pando-darwin-<arch>.zip -> ~/.local/bin/pando.
#
# Options (each has an environment variable for `curl | bash` use):
#   --version <tag>   PANDO_VERSION       release to install (default: latest)
#   --dir <path>      PANDO_INSTALL_DIR   where the binary goes (default: ~/.local/bin)
#   --no-desktop      PANDO_NO_DESKTOP=1  Linux: skip GTK/WebKitGTK packages, icon
#                                         and .desktop entry (servers, CI, containers)
#   --cli-only        PANDO_CLI_ONLY=1    macOS: install only the CLI binary, no .pkg
#   --force           PANDO_FORCE=1       reinstall even if this version is installed
#   -h, --help

set -euo pipefail

GITHUB_REPO="digiogithub/pando"
GITHUB_RELEASES="https://github.com/${GITHUB_REPO}/releases"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
ICON_URL="https://raw.githubusercontent.com/${GITHUB_REPO}/main/assets/pando-brand-v1/png/pando-icon-256.png"
CHECKSUMS_FILE="SHA256SUMS"

VERSION="${PANDO_VERSION:-}"
INSTALL_DIR="${PANDO_INSTALL_DIR:-${HOME}/.local/bin}"
NO_DESKTOP="${PANDO_NO_DESKTOP:-}"
CLI_ONLY="${PANDO_CLI_ONLY:-}"
FORCE="${PANDO_FORCE:-}"
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

usage() {
    cat <<'USAGE'
Pando installer for Linux and macOS

Usage: install.sh [options]

  --version <tag>   release to install, e.g. v1.2.7 (default: latest)   [PANDO_VERSION]
  --dir <path>      where the binary goes (default: ~/.local/bin)       [PANDO_INSTALL_DIR]
  --no-desktop      Linux: skip GTK/WebKitGTK packages, icon and
                    .desktop entry (servers, CI, containers)            [PANDO_NO_DESKTOP=1]
  --cli-only        macOS: install only the CLI binary, not the .pkg    [PANDO_CLI_ONLY=1]
  --force           reinstall even if this version is installed         [PANDO_FORCE=1]
  -h, --help        show this help
USAGE
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --version)     [[ $# -ge 2 ]] || error "--version needs a value"; VERSION="$2"; shift 2 ;;
            --version=*)   VERSION="${1#*=}"; shift ;;
            --dir)         [[ $# -ge 2 ]] || error "--dir needs a value"; INSTALL_DIR="$2"; shift 2 ;;
            --dir=*)       INSTALL_DIR="${1#*=}"; shift ;;
            --no-desktop)  NO_DESKTOP=1; shift ;;
            --cli-only)    CLI_ONLY=1; shift ;;
            --force)       FORCE=1; shift ;;
            -h|--help)     usage; exit 0 ;;
            *)             error "Unknown option: $1 (see --help)" ;;
        esac
    done
}

# ── Detect platform ──────────────────────────────────────────────────────────
detect_os() {
    case "$(uname -s)" in
        Linux)   echo "linux" ;;
        Darwin)  echo "darwin" ;;
        *)       error "Unsupported operating system: $(uname -s). On Windows use scripts/install-windows.ps1." ;;
    esac
}

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

    # Already root (containers): no sudo needed.
    local sudo="sudo"
    if [[ "$(id -u)" -eq 0 ]]; then
        sudo=""
    elif ! command -v sudo &>/dev/null; then
        warn "sudo is not available: cannot install the missing packages automatically."
        return 1
    else
        info "Requesting sudo access to install missing desktop runtime libraries..."
        sudo -v || return 1
    fi

    case "${pm}" in
        apt)
            ${sudo} apt-get update && ${sudo} apt-get install -y "${missing[@]}"
            ;;
        dnf)
            ${sudo} dnf install -y "${missing[@]}"
            ;;
        pacman)
            ${sudo} pacman -Sy --needed --noconfirm "${missing[@]}"
            ;;
        zypper)
            ${sudo} zypper --non-interactive install --auto-agree-with-licenses "${missing[@]}"
            ;;
        *)
            warn "Unsupported package manager for automatic installation: ${pm}"
            return 1
            ;;
    esac
}

warn_ubuntu_runtime_hint() {
    warn "  Ubuntu 22.04:        sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0"
    warn "  Ubuntu 24.04, 26.04: sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0t64"
}

ensure_wails_runtime_dependencies() {
    local pm
    pm="$(pkg_manager)"

    if [[ -z "${pm}" ]]; then
        warn "Could not detect a supported package manager (apt/dnf/pacman/zypper)."
        warn "Skipping automatic dependency installation for Wails desktop runtime."
        warn "Please ensure your system has GTK and WebKitGTK runtime libraries installed."
        warn_ubuntu_runtime_hint
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
        warn_ubuntu_runtime_hint
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

    # The desktop window needs these; the terminal UI, the CLI and the web UI
    # do not. So a failure here is reported and the install goes on.
    warn "Installing missing dependencies: ${missing[*]}"
    if ! run_sudo_install_cmd "${pm}" "${missing[@]}"; then
        warn "Desktop runtime libraries were not installed. 'pando desktop' will not start until they are:"
        warn "  ${missing[*]}"
        warn "Everything else (TUI, CLI, 'pando app') works without them. Use --no-desktop to skip this step."
        return 0
    fi

    local still_missing=()
    for dep in "${missing[@]}"; do
        if ! package_installed "${pm}" "${dep}"; then
            still_missing+=("${dep}")
        fi
    done

    if [[ ${#still_missing[@]} -gt 0 ]]; then
        warn "Some desktop dependencies are still missing: ${still_missing[*]}"
        warn "'pando desktop' will not start until they are installed."
        return 0
    fi

    success "Desktop runtime dependencies installed successfully."
}

# ── HTTP helpers ─────────────────────────────────────────────────────────────
have() { command -v "$1" &>/dev/null; }

fetch_stdout() {
    if have curl; then
        curl -fsSL "$1"
    else
        wget -qO- "$1"
    fi
}

# ── Get latest release version from GitHub ──────────────────────────────────
# Follows the /releases/latest redirect first: it needs no API call, so it is
# not subject to the unauthenticated API rate limit (60 requests/hour per IP,
# easily exhausted on CI runners and shared networks). The API is the fallback.
get_latest_version() {
    local url=""
    if have curl; then
        url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "${GITHUB_RELEASES}/latest" 2>/dev/null || true)"
    else
        url="$(wget --max-redirect=5 -S --spider "${GITHUB_RELEASES}/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n1 | tr -d '\r' || true)"
    fi
    case "${url}" in
        */releases/tag/*) echo "${url##*/releases/tag/}"; return 0 ;;
    esac
    fetch_stdout "${GITHUB_API}" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/'
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
    if have curl; then
        # -f fail on HTTP errors, -L follow redirects, -# progress bar
        # (no -s: it would hide the bar). --retry covers flaky networks.
        curl -fL --retry 3 --retry-delay 2 -# -o "${dest}" "${url}"
    else
        wget --tries=3 --show-progress -qO "${dest}" "${url}"
    fi
}

# ── Integrity check ──────────────────────────────────────────────────────────
# Verifies <file> against the release's SHA256SUMS. Releases published before
# that file existed are installed with a warning instead of failing.
verify_checksum() {
    local file="$1" name="$2" version="$3"
    local sums expected actual
    sums="$(fetch_stdout "${GITHUB_RELEASES}/download/${version}/${CHECKSUMS_FILE}" 2>/dev/null || true)"
    if [[ -z "${sums}" ]]; then
        warn "Release ${version} publishes no ${CHECKSUMS_FILE}: skipping the integrity check."
        return 0
    fi

    expected="$(echo "${sums}" | awk -v n="${name}" '$2 == n || $2 == "*" n { print $1 }' | head -n1)"
    if [[ -z "${expected}" ]]; then
        warn "${name} is not listed in ${CHECKSUMS_FILE}: skipping the integrity check."
        return 0
    fi

    if have sha256sum; then
        actual="$(sha256sum "${file}" | awk '{ print $1 }')"
    elif have shasum; then
        actual="$(shasum -a 256 "${file}" | awk '{ print $1 }')"
    else
        warn "Neither sha256sum nor shasum is available: skipping the integrity check."
        return 0
    fi

    if [[ "${actual}" != "${expected}" ]]; then
        error "Checksum mismatch for ${name} (expected ${expected}, got ${actual}). Aborting."
    fi
    success "Checksum verified (SHA-256)."
}

# ── Ensure required tools ────────────────────────────────────────────────────
require_tool() {
    command -v "$1" &>/dev/null || error "Required tool not found: $1. Please install it and re-run."
}

# ── PATH ─────────────────────────────────────────────────────────────────────
ensure_on_path() {
    if echo "${PATH}" | tr ':' '\n' | grep -qx "${INSTALL_DIR}"; then
        return 0
    fi

    warn "${INSTALL_DIR} is not in your PATH."
    local rc
    for rc in "${HOME}/.bashrc" "${HOME}/.zshrc" "${HOME}/.profile"; do
        if [[ -f "${rc}" ]] && ! grep -qF "${INSTALL_DIR}" "${rc}"; then
            printf '\nexport PATH="%s:$PATH"\n' "${INSTALL_DIR}" >> "${rc}"
            info "Added ${INSTALL_DIR} to PATH in ${rc}"
        fi
    done
    local fish_rc="${XDG_CONFIG_HOME:-${HOME}/.config}/fish/config.fish"
    if [[ -f "${fish_rc}" ]] && ! grep -qF "${INSTALL_DIR}" "${fish_rc}"; then
        printf '\nfish_add_path "%s"\n' "${INSTALL_DIR}" >> "${fish_rc}"
        info "Added ${INSTALL_DIR} to PATH in ${fish_rc}"
    fi
    warn "Restart your terminal or run: export PATH=\"${INSTALL_DIR}:\$PATH\""
}

# ── Install a CLI zip (Linux, and macOS with --cli-only) ─────────────────────
install_cli_zip() {
    local os="$1" arch="$2" version="$3" tmp_dir="$4"
    require_tool unzip

    local zip_name="pando-${os}-${arch}.zip"
    local download_url="${GITHUB_RELEASES}/download/${version}/${zip_name}"

    info "Downloading ${zip_name} from ${download_url} ..."
    download "${download_url}" "${tmp_dir}/${zip_name}" \
        || error "Download failed. Check that release ${version} exists and ships ${zip_name}: ${GITHUB_RELEASES}"
    verify_checksum "${tmp_dir}/${zip_name}" "${zip_name}" "${version}"

    info "Extracting archive..."
    unzip -q "${tmp_dir}/${zip_name}" -d "${tmp_dir}/extracted"

    # The archive holds one binary named after the platform.
    local pando_bin
    pando_bin="$(find "${tmp_dir}/extracted" -type f -name "pando-${os}-${arch}" | head -n1)"
    if [[ -z "${pando_bin}" ]]; then
        # Fallback for possible packaging layout/name variations
        pando_bin="$(find "${tmp_dir}/extracted" -type f -name "pando*" | head -n1)"
    fi
    [[ -z "${pando_bin}" ]] && error "Could not find the Pando binary inside ${zip_name}."

    mkdir -p "${INSTALL_DIR}"
    # install(1) unlinks the target first, so replacing a running pando is safe.
    install -m 755 "${pando_bin}" "${INSTALL_DIR}/pando"
    success "Binary installed to ${INSTALL_DIR}/pando"

    ensure_on_path
}

# ── Linux desktop integration ────────────────────────────────────────────────
install_linux_desktop_entry() {
    # Cosmetic: a missing icon or menu entry must never fail the install.
    mkdir -p "${ICON_DIR}" "${DESKTOP_DIR}" || return 0
    info "Downloading application icon..."
    if download "${ICON_URL}" "${ICON_DIR}/pando.png"; then
        success "Icon saved to ${ICON_DIR}/pando.png"
    else
        warn "Could not download the application icon; the menu entry will use a generic one."
    fi

    cat > "${DESKTOP_DIR}/pando.desktop" <<DESKTOP
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
DESKTOP
    chmod 644 "${DESKTOP_DIR}/pando.desktop"
    success ".desktop entry created at ${DESKTOP_DIR}/pando.desktop"

    if have update-desktop-database; then
        update-desktop-database "${DESKTOP_DIR}" 2>/dev/null || true
    fi
}

# ── macOS: signed and notarized .pkg ─────────────────────────────────────────
install_macos_pkg() {
    local arch="$1" version="$2" tmp_dir="$3"
    local pkg_name="pando-${version}-darwin-${arch}.pkg"
    local download_url="${GITHUB_RELEASES}/download/${version}/${pkg_name}"

    info "Downloading ${pkg_name} from ${download_url} ..."
    download "${download_url}" "${tmp_dir}/${pkg_name}" \
        || error "Download failed. Check that release ${version} exists and ships ${pkg_name}: ${GITHUB_RELEASES}"
    verify_checksum "${tmp_dir}/${pkg_name}" "${pkg_name}" "${version}"

    # Refuse a package Gatekeeper would refuse: unsigned or tampered with.
    if have pkgutil && ! pkgutil --check-signature "${tmp_dir}/${pkg_name}" >/dev/null 2>&1; then
        error "${pkg_name} has no valid Developer ID signature. Aborting."
    fi

    info "Installing Pando.app to /Applications and the launcher to /usr/local/bin (needs sudo)..."
    sudo installer -pkg "${tmp_dir}/${pkg_name}" -target / \
        || error "The macOS installer failed. You can also open the .pkg by hand: ${GITHUB_RELEASES}/tag/${version}"
    success "Pando.app installed in /Applications, launcher at /usr/local/bin/pando"
}

# ── Main ─────────────────────────────────────────────────────────────────────
main() {
    parse_args "$@"

    have curl || have wget || error "Neither curl nor wget is available. Please install one of them."

    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"

    echo -e "${BOLD}Pando installer${RESET} (${os}/${arch})"
    echo "────────────────────────────────────────"

    if [[ -z "${VERSION}" ]]; then
        info "Fetching latest release information..."
        VERSION="$(get_latest_version || true)"
        [[ -z "${VERSION}" ]] && error "Could not determine the latest release. Pass one explicitly: --version v1.2.7"
    fi
    # Release tags carry a leading v; accept `--version 1.2.7` too.
    [[ "${VERSION}" == v* ]] || VERSION="v${VERSION}"
    info "Version to install: ${VERSION}"

    local installed_version
    installed_version="$(get_installed_version)"
    if [[ -n "${installed_version}" ]]; then
        info "Currently installed version: ${installed_version}"
        if [[ "${installed_version#v}" == "${VERSION#v}" && -z "${FORCE}" ]]; then
            success "Pando ${installed_version} is already installed. Use --force to reinstall."
            exit 0
        fi
        warn "Replacing ${installed_version} with ${VERSION}"
    else
        info "No existing Pando installation found. Proceeding with fresh install."
    fi

    TMP_DIR="$(mktemp -d)"
    trap 'rm -rf "${TMP_DIR}"' EXIT

    if [[ "${os}" == "darwin" ]]; then
        if [[ -n "${CLI_ONLY}" ]]; then
            install_cli_zip "darwin" "${arch}" "${VERSION}" "${TMP_DIR}"
        else
            install_macos_pkg "${arch}" "${VERSION}" "${TMP_DIR}"
        fi
    else
        if [[ -z "${NO_DESKTOP}" ]]; then
            local distro_id distro_like
            distro_id="$(detect_distro_id)"
            distro_like="$(detect_distro_like)"
            if [[ -n "${distro_id}" ]]; then
                info "Detected distro: ${distro_id}${distro_like:+ (like: ${distro_like})}"
            fi
            ensure_wails_runtime_dependencies
        else
            info "Skipping desktop runtime libraries and menu entry (--no-desktop)."
        fi

        install_cli_zip "linux" "${arch}" "${VERSION}" "${TMP_DIR}"

        if [[ -z "${NO_DESKTOP}" ]]; then
            install_linux_desktop_entry
        fi
    fi

    echo "────────────────────────────────────────"
    if [[ -n "${installed_version}" ]]; then
        success "Pando updated: ${installed_version} → ${VERSION}"
    else
        success "Pando ${VERSION} installed successfully!"
    fi
    echo -e "${BOLD}Run:${RESET} pando --help"
}

main "$@"
