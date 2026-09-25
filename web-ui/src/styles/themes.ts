/**
 * Theme palette metadata for previews (ThemePicker swatches, window chrome).
 *
 * tokens.css is the runtime source of truth; these values are a copy of a
 * few of its hex literals, used where CSS variables cannot reach (a preview
 * of a family that is not active, the Wails window background, the
 * `meta theme-color`). `bun run check:contrast` verifies they match
 * tokens.css, so edit both together.
 */

export type ThemeFamily = 'pando' | 'paper' | 'slate' | 'forest'
export type ResolvedMode = 'light' | 'dark'
export type AccentPreset = 'gold' | 'terracotta' | 'violet' | 'blue' | 'green' | 'rose' | 'graphite'

export interface PreviewPalette {
  bg: string
  shell: string
  raised: string
  fg: string
  accent: string
}

export const FAMILY_PALETTES: Record<ThemeFamily, { label: string } & Record<ResolvedMode, PreviewPalette>> = {
  pando: {
    label: 'Pando',
    light: { bg: '#fdfcf8', shell: '#f4f1e8', raised: '#ece6d4', fg: '#10251c', accent: '#8f6310' },
    dark: { bg: '#0c1f18', shell: '#0a1a14', raised: '#16332a', fg: '#f4f1e8', accent: '#e9b949' },
  },
  paper: {
    label: 'Paper',
    light: { bg: '#faf9f5', shell: '#f3f1ea', raised: '#ece9df', fg: '#1f1e1d', accent: '#b0512e' },
    dark: { bg: '#262624', shell: '#1f1e1d', raised: '#34342f', fg: '#f4f2ec', accent: '#d97757' },
  },
  slate: {
    label: 'Slate',
    light: { bg: '#fbfcfd', shell: '#f1f4f8', raised: '#e9edf3', fg: '#0f172a', accent: '#2563eb' },
    dark: { bg: '#0f141b', shell: '#141a22', raised: '#212a36', fg: '#e6ebf2', accent: '#6ea8fe' },
  },
  forest: {
    label: 'Forest',
    light: { bg: '#fbfbf9', shell: '#f2f3ef', raised: '#eaece6', fg: '#1a1d1a', accent: '#1f6f4a' },
    dark: { bg: '#121513', shell: '#171b18', raised: '#242a25', fg: '#e8ece8', accent: '#5cb88a' },
  },
}

export const ACCENT_PALETTES: Record<AccentPreset, { label: string } & Record<ResolvedMode, string>> = {
  gold: { label: 'Gold', light: '#8f6310', dark: '#e9b949' },
  terracotta: { label: 'Terracotta', light: '#b0512e', dark: '#d97757' },
  violet: { label: 'Violet', light: '#5b43e8', dark: '#8b7cf6' },
  blue: { label: 'Blue', light: '#2563eb', dark: '#6ea8fe' },
  green: { label: 'Green', light: '#1f6f4a', dark: '#5cb88a' },
  rose: { label: 'Rose', light: '#be185d', dark: '#f472b6' },
  graphite: { label: 'Graphite', light: '#27272a', dark: '#e4e4e7' },
}
