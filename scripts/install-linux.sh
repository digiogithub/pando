#!/usr/bin/env bash
# install-linux.sh — kept for compatibility with published instructions.
#
# The installer now lives in scripts/install.sh, which covers Linux and macOS.
# This file forwards to it so `curl .../install-linux.sh | bash` keeps working.

set -euo pipefail

INSTALL_SH_URL="https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install.sh"

# Run from a checkout: use the sibling script. Piped from curl: BASH_SOURCE is
# empty, so fetch it.
self_dir=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    self_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi

if [[ -n "${self_dir}" && -f "${self_dir}/install.sh" ]]; then
    exec bash "${self_dir}/install.sh" "$@"
fi

if command -v curl &>/dev/null; then
    curl -fsSL "${INSTALL_SH_URL}" | bash -s -- "$@"
elif command -v wget &>/dev/null; then
    wget -qO- "${INSTALL_SH_URL}" | bash -s -- "$@"
else
    echo "Neither curl nor wget is available. Please install one of them." >&2
    exit 1
fi
