/**
 * Theme store (zustand) — single source of truth for the UI theme.
 *
 * Theme = family x mode x optional accent preset:
 *   family: pando | paper | slate | forest
 *   mode:   light | dark | system  (system follows prefers-color-scheme live)
 *   accent: null (family default) | gold | terracotta | violet | blue | green | rose | graphite
 *
 * The resolved state is written to <html> as data-theme-name / data-theme /
 * data-accent (see styles/tokens.css). index.html applies the same attributes
 * from localStorage before React mounts, so there is no flash on load.
 *
 * Persistence: localStorage `pando_theme` ("family-mode", mode may be
 * "system") and `pando_accent`. The backend config field `theme` keeps the
 * legacy "family-mode" string; legacy family ids are mapped on read.
 */
import { create } from 'zustand'
import { useShallow } from 'zustand/react/shallow'

import {
  ACCENT_PALETTES,
  FAMILY_PALETTES,
  type AccentPreset,
  type ResolvedMode,
  type ThemeFamily,
} from '@/styles/themes'

export type { AccentPreset, ResolvedMode, ThemeFamily }
export type ThemeModePref = ResolvedMode | 'system'
/** Kept for backwards compatibility: the resolved (light/dark) mode. */
export type ThemeMode = ResolvedMode
/** Kept for backwards compatibility with v1 code. */
export type ThemeName = ThemeFamily
export type ThemeId = `${ThemeFamily}-${ThemeModePref}`

export const THEME_FAMILIES = Object.keys(FAMILY_PALETTES) as ThemeFamily[]
export const ACCENT_PRESETS = Object.keys(ACCENT_PALETTES) as AccentPreset[]

const THEME_KEY = 'pando_theme'
const ACCENT_KEY = 'pando_accent'
const DEFAULT_FAMILY: ThemeFamily = 'pando'
const DEFAULT_MODE: ThemeModePref = 'system'

/** v1 family ids (brand-named) mapped to their v2 replacement. */
const LEGACY_FAMILIES: Record<string, ThemeFamily> = {
  claude: 'paper',
  clay: 'paper',
  starbucks: 'forest',
}

export function parseThemeId(id: string | null | undefined): { family: ThemeFamily; mode: ThemeModePref } {
  if (!id) return { family: DEFAULT_FAMILY, mode: DEFAULT_MODE }
  // Accept legacy mode-only values.
  if (id === 'light' || id === 'dark' || id === 'system') return { family: DEFAULT_FAMILY, mode: id }
  const parts = id.split('-')
  const last = parts[parts.length - 1]
  const hasMode = last === 'light' || last === 'dark' || last === 'system'
  const mode: ThemeModePref = hasMode ? (last as ThemeModePref) : DEFAULT_MODE
  const rawFamily = (hasMode ? parts.slice(0, -1) : parts).join('-')
  const family = normalizeFamily(rawFamily)
  return { family, mode }
}

/**
 * True when `id` is a WebUI theme id ("family-mode", v1 brand ids included).
 * The backend `theme` field is shared with the TUI (e.g. "pando-nobg"), so
 * only ids that pass this check should be adopted by the WebUI.
 */
export function isWebThemeId(id: string | null | undefined): boolean {
  if (!id) return false
  const parts = id.split('-')
  const last = parts[parts.length - 1]
  if (last !== 'light' && last !== 'dark' && last !== 'system') return false
  const raw = parts.slice(0, -1).join('-')
  return (THEME_FAMILIES as string[]).includes(raw) || isLegacyFamily(raw)
}

/** True when this browser already has a locally persisted theme choice. */
export function hasStoredTheme(): boolean {
  return readStorage(THEME_KEY) !== null
}

function normalizeFamily(raw: string): ThemeFamily {
  if ((THEME_FAMILIES as string[]).includes(raw)) return raw as ThemeFamily
  return isLegacyFamily(raw) ? LEGACY_FAMILIES[raw] : DEFAULT_FAMILY
}

function isLegacyFamily(raw: string): boolean {
  return Object.prototype.hasOwnProperty.call(LEGACY_FAMILIES, raw)
}

function normalizeAccent(raw: string | null | undefined): AccentPreset | null {
  return raw && (ACCENT_PRESETS as string[]).includes(raw) ? (raw as AccentPreset) : null
}

const darkQuery: MediaQueryList | null =
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)')
    : null

function systemMode(): ResolvedMode {
  return darkQuery?.matches ? 'dark' : 'light'
}

function resolve(mode: ThemeModePref): ResolvedMode {
  return mode === 'system' ? systemMode() : mode
}

function readStorage(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStorage(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key)
    else localStorage.setItem(key, value)
  } catch {
    /* storage unavailable (private mode) — theme still applies for this session */
  }
}

/** Keeps the native window chrome (Wails) and mobile browser UI in sync. */
function syncChrome(family: ThemeFamily, resolved: ResolvedMode) {
  const bg = FAMILY_PALETTES[family][resolved].bg
  // index.html paints <html> inline before CSS loads; keep it current.
  document.documentElement.style.backgroundColor = bg
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', bg)

  // Wails injects `window.runtime` in the desktop shell. Absent in browsers.
  const runtime = (window as unknown as {
    runtime?: { WindowSetBackgroundColour?: (r: number, g: number, b: number, a: number) => void }
  }).runtime
  if (runtime?.WindowSetBackgroundColour) {
    const n = parseInt(bg.slice(1), 16)
    try {
      runtime.WindowSetBackgroundColour((n >> 16) & 255, (n >> 8) & 255, n & 255, 255)
    } catch {
      /* ignore — purely cosmetic */
    }
  }
}

function applyToDocument(family: ThemeFamily, resolved: ResolvedMode, accent: AccentPreset | null) {
  const root = document.documentElement
  root.setAttribute('data-theme-name', family)
  root.setAttribute('data-theme', resolved)
  if (accent) root.setAttribute('data-accent', accent)
  else root.removeAttribute('data-accent')
  syncChrome(family, resolved)
}

interface ThemeState {
  family: ThemeFamily
  mode: ThemeModePref
  resolvedMode: ResolvedMode
  accent: AccentPreset | null
  setFamily: (family: ThemeFamily | string) => void
  setMode: (mode: ThemeModePref) => void
  setAccent: (accent: AccentPreset | null) => void
  /** Accepts "family-mode" (legacy ids mapped), or a bare mode. */
  setTheme: (id: string) => void
  /** Flips light <-> dark (from "system" it flips the currently resolved mode). */
  toggleMode: () => void
}

const initial = parseThemeId(readStorage(THEME_KEY))
const initialAccent = normalizeAccent(readStorage(ACCENT_KEY))

function commit(family: ThemeFamily, mode: ThemeModePref, accent: AccentPreset | null, persist = true) {
  const resolvedMode = resolve(mode)
  applyToDocument(family, resolvedMode, accent)
  if (persist) {
    writeStorage(THEME_KEY, `${family}-${mode}`)
    writeStorage(ACCENT_KEY, accent)
  }
  return { family, mode, resolvedMode, accent }
}

export const useThemeStore = create<ThemeState>((set, get) => ({
  ...commit(initial.family, initial.mode, initialAccent, false),
  setFamily: (family) => {
    const s = get()
    set(commit(normalizeFamily(family), s.mode, s.accent))
  },
  setMode: (mode) => {
    const s = get()
    set(commit(s.family, mode, s.accent))
  },
  setAccent: (accent) => {
    const s = get()
    set(commit(s.family, s.mode, normalizeAccent(accent)))
  },
  setTheme: (id) => {
    const s = get()
    if (id === 'light' || id === 'dark' || id === 'system') {
      set(commit(s.family, id, s.accent))
      return
    }
    const { family, mode } = parseThemeId(id)
    set(commit(family, mode, s.accent))
  },
  toggleMode: () => {
    const s = get()
    set(commit(s.family, s.resolvedMode === 'dark' ? 'light' : 'dark', s.accent))
  },
}))

// Follow the OS appearance live while mode === 'system'.
darkQuery?.addEventListener?.('change', () => {
  const s = useThemeStore.getState()
  if (s.mode !== 'system') return
  useThemeStore.setState(commit(s.family, s.mode, s.accent, false))
})

// Keep multiple tabs/windows in sync.
if (typeof window !== 'undefined') {
  window.addEventListener('storage', (e) => {
    if (e.key !== THEME_KEY && e.key !== ACCENT_KEY) return
    const { family, mode } = parseThemeId(readStorage(THEME_KEY))
    useThemeStore.setState(commit(family, mode, normalizeAccent(readStorage(ACCENT_KEY)), false))
  })
}

/**
 * Hook API. v1 fields (`themeId`, `themeName`, `themeMode`, `setTheme`,
 * `toggleMode`) are preserved; `themeMode` is the resolved light/dark mode.
 */
export function useTheme() {
  const s = useThemeStore(
    useShallow((st) => ({
      family: st.family,
      mode: st.mode,
      resolvedMode: st.resolvedMode,
      accent: st.accent,
      setFamily: st.setFamily,
      setMode: st.setMode,
      setAccent: st.setAccent,
      setTheme: st.setTheme,
      toggleMode: st.toggleMode,
    })),
  )
  return {
    ...s,
    themeId: `${s.family}-${s.mode}` as ThemeId,
    themeName: s.family,
    themeMode: s.resolvedMode,
  }
}
