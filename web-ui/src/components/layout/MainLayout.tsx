import { Outlet, useNavigate } from 'react-router-dom'
import { useEffect } from 'react'
import { useLayoutStore } from '@/stores/layoutStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useServerStore } from '@/stores/serverStore'
import { useSettingsStore } from '@/stores/settingsStore'
import { authenticate } from '@/services/auth'
import { useTheme } from '@/hooks/useTheme'
import Sidebar from './Sidebar'
import Header from './Header'
import StatusBar from './StatusBar'
import QuickMenu from '@/components/overlays/QuickMenu'
import ModelSwitcher from '@/components/overlays/ModelSwitcher'
import ConfigInitBanner from '@/components/overlays/ConfigInitBanner'

export default function MainLayout() {
  const { themeMode: theme } = useTheme()
  const { sidebarOpen, quickMenuOpen, modelSwitcherOpen, setSidebarOpen } = useLayoutStore()
  const fetchSessions = useSessionStore((s) => s.fetchSessions)
  const fetchSettings = useSettingsStore((s) => s.fetchSettings)
  const startHealthCheck = useServerStore((s) => s.startHealthCheck)
  const setConnected = useServerStore((s) => s.setConnected)
  const { setQuickMenuOpen, setModelSwitcherOpen, toggleSidebar } = useLayoutStore()
  const navigate = useNavigate()

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

  // Keyboard shortcuts
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
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [setModelSwitcherOpen, setQuickMenuOpen, toggleSidebar])

  // Close sidebar on mobile when navigating
  useEffect(() => {
    const isMobile = window.matchMedia('(max-width: 768px)').matches
    if (isMobile) {
      setSidebarOpen(false)
    }
  }, [navigate, setSidebarOpen])

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100vh',
        background: 'var(--bg)',
        overflow: 'hidden',
        position: 'relative',
      }}
    >
      <Header />
      <ConfigInitBanner />

      <div style={{ display: 'flex', flex: 1, overflow: 'hidden', position: 'relative' }}>
        {/* Sidebar — desktop: normal flow, mobile: overlay drawer */}
        {sidebarOpen && (
          <>
            {/* Mobile backdrop */}
            <div
              className="sidebar-mobile-backdrop"
              onClick={() => setSidebarOpen(false)}
              style={{
                display: 'none',
                position: 'fixed',
                inset: 0,
                zIndex: 99,
                background: 'rgba(0,0,0,0.4)',
              }}
            />
            <div className="sidebar-container" style={{ overflow: 'hidden', display: 'flex' }}>
              <Sidebar />
            </div>
          </>
        )}

        <main
          className="main-content"
          style={{
            flex: 1,
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            position: 'relative',
          }}
        >
          {/* Watermark centrado en el área de chat — Pando mascot */}
          <div className="pando-mascot-watermark">
            <img src="/pando_mascot.svg" alt="" />
          </div>
          {/* Contenido por encima de la marca de agua */}
          <div style={{ position: 'relative', zIndex: 1, flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
            <Outlet />
          </div>
        </main>
      </div>

      <StatusBar />

      {/* Overlays */}
      {quickMenuOpen && <QuickMenu />}
      {modelSwitcherOpen && <ModelSwitcher />}

      <style>{`
        @media (max-width: 768px) {
          .sidebar-mobile-backdrop {
            display: block !important;
          }
          /* sidebar itself becomes fixed drawer on mobile */
          .sidebar-container {
            position: fixed !important;
            top: 48px !important;
            left: 0 !important;
            bottom: 40px !important;
            right: 0 !important;
            z-index: 100 !important;
            width: 100% !important;
          }
          aside {
            width: 100% !important;
          }
          .main-content {
            display: ${sidebarOpen ? 'none !important' : 'flex !important'};
          }
        }
      `}</style>
    </div>
  )
}
