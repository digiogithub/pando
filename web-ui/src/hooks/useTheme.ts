import { useState, useEffect } from 'react'

export type ThemeMode = 'light' | 'dark'
export type ThemeName = 'pando' | 'claude' | 'clay' | 'starbucks'
export type ThemeId = `${ThemeName}-${ThemeMode}`

const THEME_KEY = 'pando_theme'
const DEFAULT_THEME: ThemeId = 'pando-light'

function parseThemeId(id: string): { name: ThemeName; mode: ThemeMode } {
  const parts = id.split('-')
  const mode = parts[parts.length - 1] === 'dark' ? 'dark' : 'light'
  const name = (parts.slice(0, -1).join('-') || 'pando') as ThemeName
  const validNames: ThemeName[] = ['pando', 'claude', 'clay', 'starbucks']
  return {
    name: validNames.includes(name) ? name : 'pando',
    mode,
  }
}

function applyTheme(id: string) {
  const { name, mode } = parseThemeId(id)
  document.documentElement.setAttribute('data-theme', mode)
  document.documentElement.setAttribute('data-theme-name', name)
}

export function useTheme() {
  const [themeId, setThemeIdState] = useState<ThemeId>(() => {
    const stored = localStorage.getItem(THEME_KEY) as ThemeId | null
    return stored ?? DEFAULT_THEME
  })

  useEffect(() => {
    applyTheme(themeId)
    localStorage.setItem(THEME_KEY, themeId)
  }, [themeId])

  const { name, mode } = parseThemeId(themeId)

  const setTheme = (id: string) => {
    // Accept both combined IDs ('claude-dark') and legacy mode-only ('dark' → keep current name)
    if (id === 'light' || id === 'dark') {
      const newId: ThemeId = `${name}-${id as ThemeMode}`
      setThemeIdState(newId)
    } else {
      setThemeIdState(id as ThemeId)
    }
  }

  const toggleMode = () => {
    const newMode: ThemeMode = mode === 'light' ? 'dark' : 'light'
    setThemeIdState(`${name}-${newMode}`)
  }

  return { themeId, themeName: name, themeMode: mode, setTheme, toggleMode }
}
