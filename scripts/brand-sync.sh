#!/usr/bin/env bash
# brand-sync.sh — regenerate brand-derived assets from the Pando v1 brand
# source of truth (assets/pando-brand-v1/, read-only) into the surfaces that
# consume them: the WebUI PWA icons, the desktop (Wails) app icons, the
# Linux install icon reference, and the Mesnada standalone UI favicons/logo.
#
# Idempotent: every output listed below is fully regenerated on each run, so
# running this script twice in a row produces the same result (safe to wire
# into a pre-build step later). It never modifies assets/pando-brand-v1/.
#
# Requires: ImageMagick `convert` and `inkscape` (used for crisp SVG
# rasterization of the stroke-based mark artwork).
#
# Usage: scripts/brand-sync.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRAND_DIR="${REPO_ROOT}/assets/pando-brand-v1"
BOSQUE="#0F2A20"

log() { echo "[brand-sync] $*"; }
die() { echo "[brand-sync] ERROR: $*" >&2; exit 1; }

command -v inkscape >/dev/null 2>&1 || die "inkscape is required but was not found on PATH"
command -v convert  >/dev/null 2>&1 || die "ImageMagick 'convert' is required but was not found on PATH"
[ -d "${BRAND_DIR}" ] || die "brand source directory not found: ${BRAND_DIR}"

# render_square SVG OUT SIZE
# Rasterize a square-viewBox SVG to an exact SIZE x SIZE PNG (no distortion,
# since source and target share the same 1:1 aspect ratio). Strips metadata
# so repeated runs produce byte-stable output.
render_square() {
    local svg="$1" out="$2" size="$3"
    inkscape "${svg}" --export-type=png --export-filename="${out}" \
        --export-width="${size}" --export-height="${size}" >/dev/null 2>&1
    convert "${out}" -strip "${out}"
}

# render_keep_aspect SVG OUT MAX_DIM
# Rasterize a non-square SVG (e.g. a bare mark or wordmark) constrained to
# MAX_DIM on its larger side, preserving aspect ratio (only one Inkscape
# dimension flag is passed, so it computes the other itself).
render_keep_aspect() {
    local svg="$1" out="$2" max_dim="$3"
    inkscape "${svg}" --export-type=png --export-filename="${out}" \
        --export-height="${max_dim}" >/dev/null 2>&1
    convert "${out}" -strip "${out}"
}

# flatten_opaque PNG BG
# Composite a (possibly transparent-cornered) PNG onto an opaque background
# of its own exact size, in place. Used for icon surfaces that must not have
# transparent pixels (apple-touch-icon, PWA "any"/maskable icons).
flatten_opaque() {
    local png="$1" bg="$2"
    convert "${png}" -background "${bg}" -alpha remove -alpha off -strip "${png}"
}

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

# ─────────────────────────────────────────────────────────────────────────
# 1. web-ui/public/ — PWA icons
# ─────────────────────────────────────────────────────────────────────────
WEBUI_PUBLIC="${REPO_ROOT}/web-ui/public"
mkdir -p "${WEBUI_PUBLIC}"
log "web-ui/public/ ..."

cp -f "${BRAND_DIR}/favicon.svg" "${WEBUI_PUBLIC}/favicon.svg"
cp -f "${BRAND_DIR}/pando-icon.svg" "${WEBUI_PUBLIC}/pando-icon.svg"

# pwa-icon-192.png / pwa-icon-512.png: rasterize the app icon (Bosque
# rounded-square + mark) and flatten onto Bosque so the rounded corners
# don't leave transparent pixels outside the rounded-rect shape.
render_square "${BRAND_DIR}/pando-icon.svg" "${WEBUI_PUBLIC}/pwa-icon-192.png" 192
flatten_opaque "${WEBUI_PUBLIC}/pwa-icon-192.png" "${BOSQUE}"

render_square "${BRAND_DIR}/pando-icon.svg" "${WEBUI_PUBLIC}/pwa-icon-512.png" 512
flatten_opaque "${WEBUI_PUBLIC}/pwa-icon-512.png" "${BOSQUE}"

# pwa-icon-maskable.png: full-bleed Bosque 512x512 canvas with the bare mark
# (pando-mark-light.svg, transparent bg, Marfil strokes + Álamo nodes)
# scaled to fit inside the central 80% safe zone (512 * 0.8 = 409.6px) and
# centered. We target 400px on the mark's longer side for a small margin of
# safety inside that zone.
MASK_SAFE_DIM=400
render_keep_aspect "${BRAND_DIR}/pando-mark-light.svg" "${TMP_DIR}/pando-mark.png" "${MASK_SAFE_DIM}"
convert -size 512x512 xc:"${BOSQUE}" "${TMP_DIR}/pando-mark.png" -gravity center -compose over -composite \
    -depth 8 -strip "${WEBUI_PUBLIC}/pwa-icon-maskable.png"

# apple-touch-icon.png: 180x180, opaque (iOS renders its own corner mask, so
# the source image must be a full-bleed square with no transparency).
render_square "${BRAND_DIR}/pando-icon.svg" "${WEBUI_PUBLIC}/apple-touch-icon.png" 180
flatten_opaque "${WEBUI_PUBLIC}/apple-touch-icon.png" "${BOSQUE}"

# ─────────────────────────────────────────────────────────────────────────
# 2. desktop/build/ — Wails (v2.16.0) app icons
#
# Verified against pkg/commands/build/packager.go in the vendored wails/v2
# module (go env GOMODCACHE):
#   - macOS: `wails build` ALWAYS regenerates <app>/Contents/Resources/
#     iconfile.icns from build/appicon.png (processDarwinIcon), ignoring any
#     pre-existing build/darwin/iconfile.icns. appicon.png is the only real
#     source of truth for the Wails-built .app icon.
#   - Windows: generateIcoFile() only synthesizes build/windows/icon.ico
#     from appicon.png if that file does NOT already exist; compileResources()
#     then embeds build/windows/icon.ico as-is into the .exe resources. So
#     shipping our own icon.ico here (from the brand .ico) is what actually
#     controls the desktop wrapper's Windows icon.
#   - build/darwin/iconfile.icns is NOT read by `wails build` NOR by
#     scripts/build-macos-app (which has its own independent PNG->iconutil
#     pipeline off assets/pando-brand-v1/png/pando-icon-1024.png, see that
#     script's SRC_ICON). It is also fully gitignored (desktop/build/darwin/
#     except Info.plist). We still (re)generate it here for anyone building
#     the darwin bundle by hand outside those two pipelines, but it has no
#     effect on either CI path.
# Desktop OS icons keep native transparency (macOS/Windows apply their own
# icon shape), so no flatten here.
# ─────────────────────────────────────────────────────────────────────────
log "desktop/build/ ..."
mkdir -p "${REPO_ROOT}/desktop/build/windows" "${REPO_ROOT}/desktop/build/darwin"

cp -f "${BRAND_DIR}/png/pando-icon-1024.png" "${REPO_ROOT}/desktop/build/appicon.png"
cp -f "${BRAND_DIR}/pando.ico" "${REPO_ROOT}/desktop/build/windows/icon.ico"
cp -f "${BRAND_DIR}/pando.icns" "${REPO_ROOT}/desktop/build/darwin/iconfile.icns"

# ─────────────────────────────────────────────────────────────────────────
# 3. internal/mesnada/ui/assets/ — Mesnada standalone UI favicons + logo
#
# The Mesnada UI's own background is near-black (--bg: #0b0f17), so it uses
# the *-light artwork (Marfil ink, meant for dark backgrounds), matching the
# family convention (see assets/pando-brand-v1/README.md).
# ─────────────────────────────────────────────────────────────────────────
MESNADA_ASSETS="${REPO_ROOT}/internal/mesnada/ui/assets"
mkdir -p "${MESNADA_ASSETS}"
log "internal/mesnada/ui/assets/ ..."

cp -f "${BRAND_DIR}/mesnada/mesnada.ico" "${MESNADA_ASSETS}/favicon.ico"
cp -f "${BRAND_DIR}/mesnada/png/mesnada-icon-32.png" "${MESNADA_ASSETS}/favicon.png"
cp -f "${BRAND_DIR}/mesnada/png/mesnada-icon-16.png" "${MESNADA_ASSETS}/favicon-16.png"

# Empty-state placeholder graphic (internal/mesnada/ui/index.html
# .logo-placeholder img, shown at ~60% opacity when no task is selected):
# the near-square bare mark reads better centered at large size than the
# wide wordmark lockup does, so use mesnada-mark-light.svg here.
cp -f "${BRAND_DIR}/mesnada/mesnada-mark-light.svg" "${MESNADA_ASSETS}/mesnada-mark-light.svg"
rm -f "${MESNADA_ASSETS}/logo.jpg"

log "done."
