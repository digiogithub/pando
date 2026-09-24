import type * as monacoTypes from 'monaco-editor'
import { useThemeStore } from '@/hooks/useTheme'
import { cssVarHex } from '@/lib/cssColor'

/** Name every Monaco/DiffEditor instance in the app should use. Redefined live on theme change. */
export const PANDO_MONACO_THEME = 'pando'

/** Appends an 8-digit hex alpha suffix to a 6-digit hex colour (Monaco/CSS Color 4 accept `#rrggbbaa`). */
function hexAlpha(hex: string, alpha: number): string {
  const clean = hex.replace('#', '').slice(0, 6).padEnd(6, '0')
  const a = Math.round(Math.min(1, Math.max(0, alpha)) * 255).toString(16).padStart(2, '0')
  return `#${clean}${a}`
}

function stripHash(hex: string): string {
  return hex.replace('#', '')
}

/**
 * Defines (or redefines) the `pando` Monaco theme from the current CSS
 * tokens. Call in `beforeMount` and again whenever the app theme changes so
 * every open editor picks up the new palette live.
 */
export function definePandoMonacoTheme(monacoInstance: typeof monacoTypes, dark: boolean) {
  const bg = cssVarHex('--bg', dark ? '#111113' : '#ffffff')
  const bgCard = cssVarHex('--bg-card', bg)
  const fg = cssVarHex('--fg', dark ? '#ececee' : '#18181b')
  const fgMuted = cssVarHex('--fg-muted', dark ? '#a6a6ad' : '#52525b')
  const fgFaint = cssVarHex('--fg-faint', dark ? '#85858c' : '#6e6e76')
  const border = cssVarHex('--border', dark ? '#27272b' : '#e6e6e9')
  const accent = cssVarHex('--accent', dark ? '#c9a24e' : '#8a6516')
  const danger = cssVarHex('--danger', dark ? '#f87171' : '#dc2626')
  const success = cssVarHex('--success', dark ? '#34d399' : '#15803d')
  const info = cssVarHex('--info', dark ? '#60a5fa' : '#2563eb')
  const warning = cssVarHex('--warning', dark ? '#eab308' : '#a16207')

  monacoInstance.editor.defineTheme(PANDO_MONACO_THEME, {
    base: dark ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [
      { token: 'comment', foreground: stripHash(fgFaint), fontStyle: 'italic' },
      { token: 'keyword', foreground: stripHash(accent), fontStyle: 'bold' },
      { token: 'string', foreground: stripHash(success) },
      { token: 'number', foreground: stripHash(warning) },
      { token: 'type', foreground: stripHash(info) },
      { token: 'variable', foreground: stripHash(fg) },
      { token: 'function', foreground: stripHash(info) },
      { token: 'operator', foreground: stripHash(fgMuted) },
      { token: 'delimiter', foreground: stripHash(fgMuted) },
    ],
    colors: {
      'editor.background': bg,
      'editor.foreground': fg,
      'editor.lineHighlightBackground': hexAlpha(fg, 0.045),
      'editor.selectionBackground': hexAlpha(accent, 0.28),
      'editor.inactiveSelectionBackground': hexAlpha(accent, 0.16),
      'editorCursor.foreground': accent,
      'editorLineNumber.foreground': fgFaint,
      'editorLineNumber.activeForeground': fg,
      'editorIndentGuide.background': border,
      'editorIndentGuide.activeBackground': fgFaint,
      'editor.selectionHighlightBackground': hexAlpha(accent, 0.14),
      'editorWidget.background': bgCard,
      'editorWidget.border': border,
      'editorSuggestWidget.background': bgCard,
      'editorSuggestWidget.border': border,
      'editorSuggestWidget.selectedBackground': hexAlpha(accent, 0.16),
      'scrollbarSlider.background': hexAlpha(fgMuted, 0.25),
      'scrollbarSlider.hoverBackground': hexAlpha(fgMuted, 0.35),
      'scrollbarSlider.activeBackground': hexAlpha(fgMuted, 0.45),
      'diffEditor.insertedTextBackground': hexAlpha(success, 0.14),
      'diffEditor.removedTextBackground': hexAlpha(danger, 0.14),
      'diffEditor.insertedLineBackground': hexAlpha(success, 0.08),
      'diffEditor.removedLineBackground': hexAlpha(danger, 0.08),
    },
  })
}

/**
 * Calls `apply(monacoInstance)` immediately and again whenever the app theme
 * (family / mode / accent) changes — the caller redefines + re-applies the
 * Monaco theme in `apply`. Returns an unsubscribe function.
 */
export function watchMonacoTheme(monacoInstance: typeof monacoTypes, apply: (monacoInstance: typeof monacoTypes, dark: boolean) => void): () => void {
  const isDark = () => document.documentElement.getAttribute('data-theme') === 'dark'
  apply(monacoInstance, isDark())
  return useThemeStore.subscribe(() => apply(monacoInstance, isDark()))
}
