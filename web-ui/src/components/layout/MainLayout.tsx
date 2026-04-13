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

export default function MainLayout() {
  const { theme } = useTheme()
  const { sidebarOpen, quickMenuOpen, modelSwitcherOpen, setSidebarOpen } = useLayoutStore()
  const fetchSessions = useSessionStore((s) => s.fetchSessions)
  const fetchSettings = useSettingsStore((s) => s.fetchSettings)
  const startHealthCheck = useServerStore((s) => s.startHealthCheck)
  const setConnected = useServerStore((s) => s.setConnected)
  const { setQuickMenuOpen, setModelSwitcherOpen, toggleSidebar } = useLayoutStore()
  const navigate = useNavigate()

  // Initialize auth + health check
  useEffect(() => {
    authenticate().then(() => {
      fetchSessions()
      fetchSettings()
      setConnected(true)
    }).catch(() => setConnected(false))

    const stop = startHealthCheck()
    return stop
  }, [])

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
  }, [])

  // Close sidebar on mobile when navigating
  useEffect(() => {
    const isMobile = window.matchMedia('(max-width: 768px)').matches
    if (isMobile) {
      setSidebarOpen(false)
    }
  }, [navigate])

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
            <Sidebar />
          </>
        )}

        <main
          style={{
            flex: 1,
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            position: 'relative',
          }}
        >
          {/* Watermark centrado en el área de chat — Oriental Symbol 木 */}
          <div
            style={{
              position: 'absolute',
              top: '50%',
              left: '50%',
              transform: 'translate(-50%, -52%)',
              fontSize: 320,
              fontWeight: 400,
              color: 'var(--primary)',
              opacity: theme === 'dark' ? 0.04 : 0.08,
              fontFamily: 'serif',
              userSelect: 'none',
              pointerEvents: 'none',
              zIndex: 0,
            }}
          >
            木
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
          aside {
            position: fixed !important;
            top: 48px !important;
            left: 0 !important;
            bottom: 40px !important;
            z-index: 100 !important;
            box-shadow: 4px 0 24px rgba(0,0,0,0.2) !important;
          }
        }
      `}</style>
    </div>
  )
}
