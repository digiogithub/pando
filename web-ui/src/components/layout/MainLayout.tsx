import { Outlet, useLocation } from 'react-router-dom'
import { useEffect, useMemo, useState } from 'react'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'
import { isProjectChildMode } from '@pando/client/services/api'
import { authenticate } from '@pando/client/services/auth'
import Sidebar from './Sidebar'
import Header from './Header'
import ProjectTabBar from './ProjectTabBar'
import {
  SHELL_MAIN_PANEL_ID,
  handleShellKeyboardShortcut,
  useProjectTabBarController,
} from './ProjectTabBarControls'
import StatusBar from './StatusBar'
import { MOBILE_QUERY, needsMacTrafficLightInset, readSidebarPref, useMediaQuery, writeSidebarPref } from './shellHooks'
import QuickMenu from '@/components/overlays/QuickMenu'
import ModelSwitcher from '@/components/overlays/ModelSwitcher'
import ConfigInitBanner from '@/components/overlays/ConfigInitBanner'
import SetupWizard from '@/components/setup/SetupWizard'
import NetworkErrorBanner from '@/components/shared/NetworkErrorBanner'
import PermissionDialog from '@/components/chat/PermissionDialog'
import QuestionDialog from '@/components/chat/QuestionDialog'
import { useProvidesWindowTitleBar } from '@/services/desktopWindow'
import { readWorkspaceProjectId, useProjectTabRouteSync } from '@/hooks/useProjectTabRouteSync'
import ProjectFrameHost from './ProjectFrameHost'
import '@/styles/shell.css'

export default function MainLayout() {
  const { sidebarOpen, quickMenuOpen, modelSwitcherOpen, setSidebarOpen } = useLayoutStore()
  // Simple chat mode keeps this shell but trims it to sessions + settings.
  const simple = useLayoutStore((s) => s.chatMode === 'simple')
  const hydrateChatMode = useLayoutStore((s) => s.hydrateChatMode)
  const fetchSessions = useSessionStore((s) => s.fetchSessions)
  const fetchSettings = useSettingsStore((s) => s.fetchSettings)
  const hydrateLanguage = useSettingsStore((s) => s.hydrateLanguage)
  const startHealthCheck = useServerStore((s) => s.startHealthCheck)
  const setConnected = useServerStore((s) => s.setConnected)
  const connectProjectEvents = useProjectStore((s) => s.connectEvents)
  const disconnectProjectEvents = useProjectStore((s) => s.disconnectEvents)
  const fetchWorkspace = useProjectStore((s) => s.fetchWorkspace)
  const projectEventSource = useProjectStore((s) => s._es)
  const { setQuickMenuOpen, setModelSwitcherOpen, toggleSidebar } = useLayoutStore()
  const location = useLocation()
  const isMobile = useMediaQuery(MOBILE_QUERY)
  const projectChildMode = isProjectChildMode()
  const projectTabBar = useProjectTabBarController()
  const [macInset] = useState(needsMacTrafficLightInset)
  // The header carries the desktop window controls; no standalone bar needed.
  useProvidesWindowTitleBar()
  useProjectTabRouteSync()
  const workspaceProjectId = readWorkspaceProjectId(location.pathname)
  const workspaceRoute = Boolean(workspaceProjectId)
  const shortcutActions = useMemo(
    () => ({
      openQuickMenu: () => setQuickMenuOpen(true),
      openModelSwitcher: () => setModelSwitcherOpen(true),
      toggleSidebar,
      toggleAutoApprove: () => {
        const { activeSessionId, toggleAutoApprove } = useSessionStore.getState()
        if (activeSessionId) {
          void toggleAutoApprove(activeSessionId)
        }
      },
    }),
    [setModelSwitcherOpen, setQuickMenuOpen, toggleSidebar],
  )

  // Initialize auth + health check
  useEffect(() => {
    void authenticate().then(() => {
      fetchSessions()
      fetchSettings()
      void useModelAutoModeStore.getState().hydrateAutoSelected()
      void hydrateChatMode()
      void hydrateLanguage()
      setConnected(true)
    }).catch(() => setConnected(false))

    const stop = startHealthCheck()
    return stop
  }, [fetchSessions, fetchSettings, hydrateChatMode, hydrateLanguage, setConnected, startHealthCheck])

  // Keyboard shortcuts (the theme toggle Ctrl/Cmd+Shift+L is global, in App).
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (handleShellKeyboardShortcut(e, projectTabBar, shortcutActions)) {
        e.preventDefault()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [projectTabBar, shortcutActions])

  useEffect(() => {
    if (projectChildMode) return
    if (!projectEventSource) {
      connectProjectEvents()
    }
  }, [connectProjectEvents, projectChildMode, projectEventSource])

  useEffect(() => {
    if (projectChildMode) return
    void fetchWorkspace()
    void useProjectTabsStore.getState().restore()
    return () => {
      disconnectProjectEvents()
    }
  }, [disconnectProjectEvents, fetchWorkspace, projectChildMode])

  // Desktop: restore the stored expanded/rail preference; mobile: start closed.
  useEffect(() => {
    if (isMobile) {
      setSidebarOpen(false)
      return
    }
    const stored = readSidebarPref()
    if (stored !== null) setSidebarOpen(stored)
  }, [isMobile, setSidebarOpen])

  // Persist the desktop preference on actual changes only (a mount-time write
  // would clobber the stored value before it is restored).
  useEffect(
    () =>
      useLayoutStore.subscribe((s, prev) => {
        if (s.sidebarOpen !== prev.sidebarOpen && !window.matchMedia(MOBILE_QUERY).matches) {
          writeSidebarPref(s.sidebarOpen)
        }
      }),
    [],
  )

  // Close the drawer on mobile when navigating.
  useEffect(() => {
    if (window.matchMedia(MOBILE_QUERY).matches) {
      setSidebarOpen(false)
    }
  }, [location.pathname, setSidebarOpen])

  // Esc closes the mobile drawer.
  useEffect(() => {
    if (!isMobile || !sidebarOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setSidebarOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [isMobile, sidebarOpen, setSidebarOpen])

  const sidebarVariant = isMobile ? 'drawer' : sidebarOpen ? 'full' : 'rail'

  return (
    <div className="shell" data-mac-inset={macInset || undefined}>
      <Header isMobile={isMobile} simple={simple} hideSidebarToggle={workspaceRoute} />
      <NetworkErrorBanner />
      {!workspaceRoute && <ConfigInitBanner />}

      <div className="shell-body">
        {!workspaceRoute && isMobile ? (
          sidebarOpen && (
            <>
              <div className="shell-scrim" onClick={() => setSidebarOpen(false)} aria-hidden="true" />
              <Sidebar variant="drawer" simple={simple} />
            </>
          )
        ) : !workspaceRoute && simple ? (
          // No rail in simple mode: collapsed means hidden, the title bar
          // toggle brings the sessions panel back.
          sidebarOpen && <Sidebar variant="full" simple />
        ) : !workspaceRoute ? (
          <Sidebar variant={sidebarVariant} />
        ) : null}

        <main className={workspaceRoute ? 'shell-main shell-main--workspace' : 'shell-main'} id={SHELL_MAIN_PANEL_ID}>
          {!projectChildMode && (
            <ProjectFrameHost controller={projectTabBar} shortcutActions={shortcutActions} />
          )}
          <div
            className={workspaceRoute ? 'shell-main-inner shell-main-inner--workspace' : 'shell-main-inner'}
          >
            <Outlet />
          </div>
        </main>
      </div>

      {!simple && !workspaceRoute && <StatusBar />}
      <ProjectTabBar simple={simple} controller={projectTabBar} />

      {/* Overlays */}
      {quickMenuOpen && <QuickMenu />}
      {modelSwitcherOpen && <ModelSwitcher />}
      <PermissionDialog />
      <QuestionDialog />
      <SetupWizard />
      {projectTabBar.dialogs}
    </div>
  )
}
