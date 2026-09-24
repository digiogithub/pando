import type { ITheme } from '@xterm/xterm'
import { useThemeStore } from '@/hooks/useTheme'

function cssVar(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

/**
 * Builds an xterm.js theme from the live CSS tokens: surfaces from
 * tokens.css (`--bg-card`, `--fg`, `--accent`, `--accent-soft`), the ANSI 16
 * from the `--term-*` palette in styles/terminal.css (tuned per light/dark).
 */
export function getXtermTheme(): ITheme {
  return {
    background: cssVar('--bg-card', '#ffffff'),
    foreground: cssVar('--fg', '#18181b'),
    cursor: cssVar('--accent', '#8a6516'),
    cursorAccent: cssVar('--bg-card', '#ffffff'),
    selectionBackground: cssVar('--accent-soft', 'rgba(138,101,22,0.2)'),
    black: cssVar('--term-black', '#1c1c1f'),
    red: cssVar('--term-red', '#c53030'),
    green: cssVar('--term-green', '#1f8a55'),
    yellow: cssVar('--term-yellow', '#a16207'),
    blue: cssVar('--term-blue', '#2563eb'),
    magenta: cssVar('--term-magenta', '#9333ea'),
    cyan: cssVar('--term-cyan', '#0e7490'),
    white: cssVar('--term-white', '#d4d4d8'),
    brightBlack: cssVar('--term-bright-black', '#6e6e76'),
    brightRed: cssVar('--term-bright-red', '#e35d61'),
    brightGreen: cssVar('--term-bright-green', '#34a866'),
    brightYellow: cssVar('--term-bright-yellow', '#ca8a04'),
    brightBlue: cssVar('--term-bright-blue', '#3b82f6'),
    brightMagenta: cssVar('--term-bright-magenta', '#a855f7'),
    brightCyan: cssVar('--term-bright-cyan', '#06b6d4'),
    brightWhite: cssVar('--term-bright-white', '#f4f4f5'),
  }
}

/**
 * Calls `apply` with a fresh xterm theme immediately, then again every time
 * the app theme (family / mode / accent) changes. Returns an unsubscribe fn.
 */
export function watchXtermTheme(apply: (theme: ITheme) => void): () => void {
  apply(getXtermTheme())
  return useThemeStore.subscribe(() => apply(getXtermTheme()))
}
