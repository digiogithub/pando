/**
 * Interface font size ("UI scale") — small | default | large.
 *
 * Local-only (never sent to the backend, unlike the `theme` config field):
 * persisted in localStorage under `pando_ui_size` and applied as
 * `data-ui-size` on <html>, which `styles/settings.css` maps to a couple of
 * `--text-*` token overrides. Kept as a tiny standalone module (rather than
 * folded into `hooks/useTheme.ts`, which is outside this agent's area)
 * mirroring its boot-time-apply / localStorage-sync shape.
 *
 * Applied before first paint by the inline script in `index.html` (mirrors
 * the theme boot script, kept in sync manually — see the comment there);
 * this module re-applies on load as a fallback and is what the Appearance
 * settings panel reads and writes.
 */

const KEY = 'pando_ui_size'

export type UIScale = 'small' | 'default' | 'large'

const VALID: readonly UIScale[] = ['small', 'default', 'large']

function isUIScale(v: string | null): v is UIScale {
  return v !== null && (VALID as readonly string[]).includes(v)
}

function readStorage(): UIScale {
  try {
    const v = localStorage.getItem(KEY)
    return isUIScale(v) ? v : 'default'
  } catch {
    return 'default'
  }
}

function apply(scale: UIScale) {
  const root = document.documentElement
  if (scale === 'default') root.removeAttribute('data-ui-size')
  else root.setAttribute('data-ui-size', scale)
}

/** Current UI scale (localStorage, falling back to "default"). */
export function getUIScale(): UIScale {
  return readStorage()
}

/** Persists and immediately applies the UI scale. */
export function setUIScale(scale: UIScale) {
  try {
    if (scale === 'default') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, scale)
  } catch {
    /* storage unavailable (private mode) — still applies for this session */
  }
  apply(scale)
}

if (typeof document !== 'undefined') {
  // Fallback in case the index.html boot script could not run; harmless
  // (and a no-op) once it already has.
  apply(readStorage())
}

if (typeof window !== 'undefined') {
  // Keep multiple tabs/windows in sync, like useTheme.ts does for the theme.
  window.addEventListener('storage', (e) => {
    if (e.key === KEY) apply(readStorage())
  })
}
