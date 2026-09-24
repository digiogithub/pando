import { useEffect, useState } from 'react'
import { isDesktop } from '@/services/desktop'

/** Viewport width at or below which the sidebar becomes an overlay drawer. */
export const MOBILE_QUERY = '(max-width: 768px)'

/** Desktop sidebar preference (expanded vs icon rail). Mobile never persists. */
export const SIDEBAR_KEY = 'pando_sidebar_open'

export function isMobileViewport(): boolean {
  return typeof window !== 'undefined' && window.matchMedia(MOBILE_QUERY).matches
}

/** Live `matchMedia` subscription. */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  )
  useEffect(() => {
    const mql = window.matchMedia(query)
    const onChange = () => setMatches(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])
  return matches
}

export function readSidebarPref(): boolean | null {
  try {
    const v = localStorage.getItem(SIDEBAR_KEY)
    return v === null ? null : v === 'true'
  } catch {
    return null
  }
}

export function writeSidebarPref(open: boolean): void {
  try {
    localStorage.setItem(SIDEBAR_KEY, String(open))
  } catch {
    // Storage unavailable (private mode): the preference just is not kept.
  }
}

/** True on Apple platforms (used for the Cmd/Ctrl shortcut hint). */
export const isMacPlatform: boolean =
  typeof navigator !== 'undefined' &&
  /Mac|iPhone|iPad/i.test(
    (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ||
      navigator.platform ||
      navigator.userAgent,
  )

/**
 * True when the Wails desktop window on macOS draws the web content under the
 * native traffic lights (hidden/inset title bar). With a regular native title
 * bar the outer and inner heights differ by the title bar height, so no inset
 * is reserved.
 */
export function needsMacTrafficLightInset(): boolean {
  if (!isDesktop || !isMacPlatform || typeof window === 'undefined') return false
  return window.outerHeight - window.innerHeight < 4
}
