import { useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { isProjectChildMode } from '@pando/client/services/api'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'

const PROJECT_WORKSPACE_ROUTE = /^\/projects\/([^/]+)\/workspace\/?$/

export function projectWorkspacePath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/workspace`
}

export function readWorkspaceProjectId(pathname: string): string | null {
  const match = PROJECT_WORKSPACE_ROUTE.exec(pathname)
  if (!match) return null

  try {
    return decodeURIComponent(match[1] ?? '')
  } catch {
    return match[1] ?? null
  }
}

/**
 * Keeps the active project tab and the URL consistent.
 *
 * Each side wins only for the changes it originates, so the two never chase
 * each other: a navigation (links, browser back/forward, a reload) makes the
 * route's tab active, and a focus change coming from the store (tab bar click,
 * a tab that just finished opening) moves the URL. On mount the URL wins, so a
 * reload stays on the page it was showing.
 */
export function useProjectTabRouteSync() {
  const navigate = useNavigate()
  const location = useLocation()
  const activeTabId = useProjectTabsStore((state) => state.activeTabId)

  const routeTabId = readWorkspaceProjectId(location.pathname) ?? 'main'
  const currentRoute = `${location.pathname}${location.search}${location.hash}`
  const mounted = useRef(false)

  // Route -> store.
  useEffect(() => {
    if (isProjectChildMode()) return
    const store = useProjectTabsStore.getState()

    if (routeTabId === 'main') {
      if (store.lastMainRoute !== currentRoute) {
        useProjectTabsStore.setState({ lastMainRoute: currentRoute })
      }
    } else if (!store.tabs.some((tab) => tab.projectId === routeTabId)) {
      void store.openTab(routeTabId)
    }

    if (store.activeTabId !== routeTabId) {
      store.focusTab(routeTabId)
    }
  }, [currentRoute, routeTabId])

  // Store -> route. Runs only when the active tab changes, never on navigation,
  // and reads the live store value so a focus the route effect has just applied
  // (including StrictMode's double effect run) never sends the URL back.
  useEffect(() => {
    if (!mounted.current) {
      mounted.current = true
      return
    }
    if (isProjectChildMode()) return
    const { activeTabId: current, lastMainRoute } = useProjectTabsStore.getState()
    if (current === routeTabId) return

    if (current === 'main') {
      navigate(lastMainRoute || '/')
    } else {
      navigate(projectWorkspacePath(current))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only a store-side focus change may move the URL
  }, [activeTabId])
}
