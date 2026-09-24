import { Outlet, useLocation } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { authenticate } from '@pando/client/services/auth'
import Sidebar from './Sidebar'
import Header from './Header'
import StatusBar from './StatusBar'
import { MOBILE_QUERY, needsMacTrafficLightInset, readSidebarPref, useMediaQuery, writeSidebarPref } from './shellHooks'
import QuickMenu from '@/components/overlays/QuickMenu'
import ModelSwitcher from '@/components/overlays/ModelSwitcher'
import ConfigInitBanner from '@/components/overlays/ConfigInitBanner'
import NetworkErrorBanner from '@/components/shared/NetworkErrorBanner'
import PermissionDialog from '@/components/chat/PermissionDialog'
import QuestionDialog from '@/components/chat/QuestionDialog'
import '@/styles/shell.css'

export default function MainLayout() {
  const { sidebarOpen, quickMenuOpen, modelSwitcherOpen, setSidebarOpen } = useLayoutStore()
  const fetchSessions = useSessionStore((s) => s.fetchSessions)
  const fetchSettings = useSettingsStore((s) => s.fetchSettings)
  const startHealthCheck = useServerStore((s) => s.startHealthCheck)
  const setConnected = useServerStore((s) => s.setConnected)
  const { setQuickMenuOpen, setModelSwitcherOpen, toggleSidebar } = useLayoutStore()
  const location = useLocation()
  const isMobile = useMediaQuery(MOBILE_QUERY)
  const [macInset] = useState(needsMacTrafficLightInset)

  // Initialize auth + health check
  useEffect(() => {
    void authenticate().then(() => {
      fetchSessions()
      fetchSettings()
      setConnected(true)
    }).catch(() => setConnected(false))

    const stop = startHealthCheck()
    return stop
  }, [fetchSessions, fetchSettings, setConnected, startHealthCheck])

  // Keyboard shortcuts (the theme toggle Ctrl/Cmd+Shift+L is global, in App).
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === 'p') {
        e.preventDefault()
        setQuickMenuOpen(true)
      }
      if (e.ctrlKey && e.key === 'o') {
        e.preventDefault()
        setModelSwitcherOpen(true)
      }
      if (e.ctrlKey && e.key === 'b') {
        e.preventDefault()
        toggleSidebar()
      }
      // Shift+Tab toggles per-session auto-approve ("auto mode").
      if (e.shiftKey && e.key === 'Tab') {
        const { activeSessionId, toggleAutoApprove } = useSessionStore.getState()
        if (activeSessionId) {
          e.preventDefault()
          void toggleAutoApprove(activeSessionId)
        }
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [setModelSwitcherOpen, setQuickMenuOpen, toggleSidebar])

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
      <Header isMobile={isMobile} />
      <NetworkErrorBanner />
      <ConfigInitBanner />

      <div className="shell-body">
        {isMobile ? (
          sidebarOpen && (
            <>
              <div className="shell-scrim" onClick={() => setSidebarOpen(false)} aria-hidden="true" />
              <Sidebar variant="drawer" />
            </>
          )
        ) : (
          <Sidebar variant={sidebarVariant} />
        )}

        <main className="shell-main">
          <div className="shell-main-inner">
            <Outlet />
          </div>
        </main>
      </div>

      <StatusBar />

      {/* Overlays */}
      {quickMenuOpen && <QuickMenu />}
      {modelSwitcherOpen && <ModelSwitcher />}
      <PermissionDialog />
      <QuestionDialog />
    </div>
  )
}
