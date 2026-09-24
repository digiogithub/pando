/**
 * Helpers to read colour tokens from CSS and turn them into the strict
 * `#rrggbb` / `#rrggbbaa` form that Monaco requires.
 *
 * Token values cannot be trusted to be 6-digit hex: the production CSS
 * minifier shortens `#ffffff` to `#fff`, and tokens may be written as
 * `rgb()`, `color-mix()` or named colours. Monaco throws
 * "Illegal value for token color" on anything but 6/8-digit hex.
 */

let probe: HTMLSpanElement | null = null

function hex2(n: number): string {
  return Math.round(Math.min(255, Math.max(0, n))).toString(16).padStart(2, '0')
}

/** Parses `rgb()/rgba()` or `color(srgb r g b / a)` computed values. */
function parseComputed(value: string): string | null {
  let m = value.match(/^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)(?:\s*[,/]\s*([\d.]+%?))?\s*\)$/i)
  if (m) {
    const alpha = m[4] === undefined ? 1 : m[4].endsWith('%') ? parseFloat(m[4]) / 100 : parseFloat(m[4])
    return `#${hex2(+m[1])}${hex2(+m[2])}${hex2(+m[3])}${alpha < 1 ? hex2(alpha * 255) : ''}`
  }
  m = value.match(/^color\(srgb\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)(?:\s*\/\s*([\d.]+%?))?\s*\)$/i)
  if (m) {
    const alpha = m[4] === undefined ? 1 : m[4].endsWith('%') ? parseFloat(m[4]) / 100 : parseFloat(m[4])
    return `#${hex2(+m[1] * 255)}${hex2(+m[2] * 255)}${hex2(+m[3] * 255)}${alpha < 1 ? hex2(alpha * 255) : ''}`
  }
  return null
}

/** Normalises any CSS colour to `#rrggbb` (or `#rrggbbaa` when translucent). */
export function toHexColor(value: string, fallback: string): string {
  const v = value.trim()
  if (/^#[0-9a-f]{6}([0-9a-f]{2})?$/i.test(v)) return v.toLowerCase()
  const short = v.match(/^#([0-9a-f])([0-9a-f])([0-9a-f])([0-9a-f])?$/i)
  if (short) return `#${short.slice(1).filter(Boolean).map((c) => c + c).join('')}`.toLowerCase()
  if (!v || typeof document === 'undefined') return fallback
  // Let the browser resolve rgb(), named colours, color-mix(), etc.
  if (!probe) {
    probe = document.createElement('span')
    probe.style.display = 'none'
    document.body.appendChild(probe)
  }
  probe.style.color = ''
  probe.style.color = v
  if (!probe.style.color) return fallback
  return parseComputed(getComputedStyle(probe).color) ?? fallback
}

/** Reads a CSS custom property from :root and returns it as strict hex. */
export function cssVarHex(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  return toHexColor(getComputedStyle(document.documentElement).getPropertyValue(name), fallback)
}
