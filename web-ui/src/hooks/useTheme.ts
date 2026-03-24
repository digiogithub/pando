import { useState, useEffect } from 'react'

type Theme = 'light' | 'dark'

const THEME_KEY = 'pando_theme'

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(() => {
    const stored = localStorage.getItem(THEME_KEY) as Theme | null
    return stored ?? 'light'
  })

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem(THEME_KEY, theme)
  }, [theme])

  const toggleTheme = () => setThemeState((t) => (t === 'light' ? 'dark' : 'light'))
  const setTheme = (t: Theme) => setThemeState(t)

  return { theme, toggleTheme, setTheme }
}
