import { Outlet, useNavigate } from 'react-router-dom'
import { useEffect } from 'react'
import { useLayoutStore } from '@/stores/layoutStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useServerStore } from '@/stores/serverStore'
import { authenticate } from '@/services/auth'
import Sidebar from './Sidebar'
import Header from './Header'
import StatusBar from './StatusBar'

export default function MainLayout() {
  const { sidebarOpen } = useLayoutStore()
  const fetchSessions = useSessionStore((s) => s.fetchSessions)
  const startHealthCheck = useServerStore((s) => s.startHealthCheck)
  const setConnected = useServerStore((s) => s.setConnected)
  const { setQuickMenuOpen, setModelSwitcherOpen, toggleSidebar } = useLayoutStore()
  const navigate = useNavigate()

  // Initialize auth + health check
  useEffect(() => {
    authenticate().then(() => {
      fetchSessions()
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

      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        {sidebarOpen && <Sidebar />}

        <main
          style={{
            flex: 1,
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            position: 'relative',
          }}
        >
          <Outlet />
        </main>
      </div>

      <StatusBar />

      {/* Watermark */}
      <div
        style={{
          position: 'absolute',
          bottom: 60,
          right: 40,
          width: 400,
          height: 400,
          backgroundImage: 'url(/pando-logo-watermark.png)',
          backgroundSize: 'contain',
          backgroundRepeat: 'no-repeat',
          backgroundPosition: 'center',
          opacity: 0.04,
          pointerEvents: 'none',
          zIndex: 0,
        }}
      />
    </div>
  )
}
