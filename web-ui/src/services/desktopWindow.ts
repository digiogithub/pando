/**
 * Window chrome for the frameless Wails desktop shell.
 *
 * The desktop window has no native title bar: the WebUI draws its own (drag
 * region + minimise to tray, maximise, close). The UI is served by the Pando
 * server, not by the Wails asset server, so the Wails runtime is not there at
 * module load: the desktop shell injects it once the document has loaded, sets
 * `window.__PANDO_DESKTOP_SHELL__` and fires `pando:desktop-shell`. Everything
 * here is therefore reactive, and a plain browser tab never sees any of it.
 */
import { useEffect, useState, useSyncExternalStore, type MouseEvent } from 'react'

const SHELL_EVENT = 'pando:desktop-shell'
const NAVIGATE_EVENT = 'pando:desktop-navigate'

interface WailsRuntime {
  WindowMinimise?: () => void
  WindowToggleMaximise?: () => void
  WindowIsMaximised?: () => Promise<boolean>
  WindowSetTitle?: (title: string) => void
  Quit?: () => void
}

interface DesktopAppBindings {
  MinimiseToTray?: () => Promise<void>
  TrayAvailable?: () => Promise<boolean>
  CloseWindow?: () => Promise<void>
  QuitApp?: () => Promise<void>
  SyncShellState?: (payload: string) => Promise<void>
}

export interface DesktopProjectTabState {
  projectId: string
  name: string
}

export interface DesktopShellState {
  title: string
  activeTabId: string
  projectTabs: DesktopProjectTabState[]
}

type ShellWindow = Window & {
  __PANDO_DESKTOP_SHELL__?: { frameless?: boolean }
  __PANDO_DESKTOP_NAV__?: boolean
  runtime?: WailsRuntime
  go?: { desktop?: { App?: DesktopAppBindings } }
}

const w = (): ShellWindow | null => (typeof window === 'undefined' ? null : (window as ShellWindow))

function isShellActive(): boolean {
  return !!w()?.__PANDO_DESKTOP_SHELL__?.frameless
}

function subscribeShell(onChange: () => void): () => void {
  window.addEventListener(SHELL_EVENT, onChange)
  return () => window.removeEventListener(SHELL_EVENT, onChange)
}

/** True inside the frameless desktop window, once its runtime is injected. */
export function useDesktopShell(): boolean {
  return useSyncExternalStore(subscribeShell, isShellActive, () => false)
}

/** Hides the window into the tray (taskbar minimise when no tray is live). */
export function minimiseWindow(): void {
  const win = w()
  const app = win?.go?.desktop?.App
  if (app?.MinimiseToTray) {
    void app.MinimiseToTray()
    return
  }
  win?.runtime?.WindowMinimise?.()
}

export function toggleMaximiseWindow(): void {
  w()?.runtime?.WindowToggleMaximise?.()
}

export function closeWindow(): void {
  const win = w()
  const app = win?.go?.desktop?.App
  if (app?.CloseWindow) {
    void app.CloseWindow()
    return
  }
  if (app?.QuitApp) {
    void app.QuitApp()
    return
  }
  win?.runtime?.Quit?.()
}

export function syncDesktopShellState(state: DesktopShellState): void {
  const win = w()
  const title = state.title.trim() || 'Pando'
  if (win?.runtime?.WindowSetTitle) {
    win.runtime.WindowSetTitle(title)
  } else if (typeof document !== 'undefined') {
    document.title = title
  }
  const app = win?.go?.desktop?.App
  if (!app?.SyncShellState) return
  void app.SyncShellState(
    JSON.stringify({
      title,
      activeTabId: state.activeTabId,
      projectTabs: state.projectTabs,
    } satisfies DesktopShellState),
  )
}

/**
 * Double-clicking an empty part of a title bar toggles maximise, as native
 * title bars do. Clicks on controls inside the bar are ignored.
 */
export function onTitleBarDoubleClick(e: MouseEvent<HTMLElement>): void {
  const target = e.target as HTMLElement
  if (target.closest('button, a, input, select, textarea, [role="button"], [role="menu"], [role="listbox"]')) return
  toggleMaximiseWindow()
}

/** Live maximised state, refreshed on every resize. */
export function useWindowMaximised(enabled: boolean): boolean {
  const [maximised, setMaximised] = useState(false)
  useEffect(() => {
    if (!enabled) return
    let alive = true
    const refresh = () => {
      const probe = w()?.runtime?.WindowIsMaximised
      if (!probe) return
      probe().then((v) => { if (alive) setMaximised(!!v) }).catch(() => {})
    }
    refresh()
    window.addEventListener('resize', refresh)
    return () => {
      alive = false
      window.removeEventListener('resize', refresh)
    }
  }, [enabled])
  return maximised
}

/** Whether the tray icon is live, which decides the minimise button's label. */
export function useTrayAvailable(enabled: boolean): boolean {
  const [available, setAvailable] = useState(false)
  useEffect(() => {
    if (!enabled) return
    let alive = true
    const probe = w()?.go?.desktop?.App?.TrayAvailable
    probe?.().then((v) => { if (alive) setAvailable(!!v) }).catch(() => {})
    return () => { alive = false }
  }, [enabled])
  return available
}

/**
 * Routes in-app navigation requests from the desktop shell (e.g. the tray's
 * "Settings" entry) to the WebUI router, so they do not reload the page.
 */
export function useDesktopNavigation(navigate: (path: string) => void): void {
  useEffect(() => {
    const win = w()
    if (!win) return
    const onNavigate = (e: Event) => {
      const path = (e as CustomEvent<unknown>).detail
      if (typeof path === 'string' && path.startsWith('/')) navigate(path)
    }
    window.addEventListener(NAVIGATE_EVENT, onNavigate)
    win.__PANDO_DESKTOP_NAV__ = true
    return () => {
      window.removeEventListener(NAVIGATE_EVENT, onNavigate)
      win.__PANDO_DESKTOP_NAV__ = false
    }
  }, [navigate])
}

/* ── Which surface draws the title bar ─────────────────────────────────── */

// MainLayout's header carries the window controls itself. Routes outside it
// (the editor) and the splash/login screens get a slim standalone bar
// instead; this counter tells the two apart.
let shellHeaders = 0
const headerListeners = new Set<() => void>()

function subscribeHeaders(onChange: () => void): () => void {
  headerListeners.add(onChange)
  return () => headerListeners.delete(onChange)
}

/** Called by a layout that renders its own title bar with window controls. */
export function useProvidesWindowTitleBar(): void {
  useEffect(() => {
    shellHeaders++
    headerListeners.forEach((l) => l())
    return () => {
      shellHeaders--
      headerListeners.forEach((l) => l())
    }
  }, [])
}

export function useHasWindowTitleBar(): boolean {
  return useSyncExternalStore(subscribeHeaders, () => shellHeaders > 0, () => false)
}
