import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faComments, faFileLines, faNetworkWired,
  faCamera, faStar, faCog, faMoon, faSun,
  faChevronLeft, faChevronRight,
} from '@fortawesome/free-solid-svg-icons'
import { useLayoutStore } from '@/stores/layoutStore'
import { useTheme } from '@/hooks/useTheme'
import { useServerStore } from '@/stores/serverStore'

const TABS = [
  { path: '/', label: 'Chat', icon: faComments, end: true },
  { path: '/orchestrator', label: 'Orchestrator', icon: faNetworkWired },
  { path: '/evaluator', label: 'Evaluator', icon: faStar },
  { path: '/snapshots', label: 'Snapshots', icon: faCamera },
  { path: '/logs', label: 'Logs', icon: faFileLines },
]

export default function Header() {
  const { toggleSidebar, sidebarOpen } = useLayoutStore()
  const { theme, toggleTheme } = useTheme()
  const connected = useServerStore((s) => s.connected)
  const [version, setVersion] = useState<string>('')

  useEffect(() => {
    fetch('/health')
      .then((r) => r.json())
      .then((d) => {
        if (d.version && d.version !== 'unknown') {
          // Strip build metadata (+dirty, +...) and ensure single "v" prefix
          const clean = d.version.replace(/\+.*$/, '')
          setVersion(clean.startsWith('v') ? clean : `v${clean}`)
        }
      })
      .catch(() => {})
  }, [])

  return (
    <header
      style={{
        display: 'flex',
        alignItems: 'stretch',
        height: 48,
        borderBottom: '1px solid var(--border)',
        background: 'var(--sidebar-bg)',
        paddingRight: '0.75rem',
        flexShrink: 0,
        zIndex: 10,
      }}
    >
      {/* Toggle sidebar */}
      <button
        onClick={toggleSidebar}
        title="Toggle sidebar (Ctrl+B)"
        style={{
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--fg-muted)',
          padding: '0 0.625rem',
          display: 'flex',
          alignItems: 'center',
          flexShrink: 0,
        }}
      >
        <FontAwesomeIcon
          icon={sidebarOpen ? faChevronLeft : faChevronRight}
          style={{ fontSize: 12 }}
        />
      </button>

      {/* Logo group: favicon + PANDO + version */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '0 20px 0 8px',
          flexShrink: 0,
          borderRight: '1px solid var(--border)',
        }}
      >
        <img
          src="/pando-favicon.svg"
          alt="Pando"
          style={{ width: 22, height: 22, flexShrink: 0 }}
        />
        <span
          style={{
            fontWeight: 800,
            fontSize: 18,
            color: 'var(--primary)',
            letterSpacing: '0.3em',
            lineHeight: 1,
          }}
        >
          PANDO
        </span>
        {version && (
          <span
            style={{
              fontSize: 11,
              fontWeight: 400,
              color: 'var(--fg-dim)',
              fontFamily: 'Inter, sans-serif',
              marginTop: 1,
            }}
          >
            {version}
          </span>
        )}
      </div>

      {/* Nav tabs — underline style */}
      <nav
        style={{ display: 'flex', alignItems: 'stretch', flex: 1, gap: 0 }}
        className="header-nav"
      >
        {TABS.map((tab) => (
          <NavLink
            key={tab.label}
            to={tab.path}
            end={tab.end}
            title={tab.label}
            style={({ isActive }) => ({
              display: 'flex',
              alignItems: 'center',
              gap: '0.375rem',
              padding: '0 0.875rem',
              textDecoration: 'none',
              fontSize: 13,
              fontWeight: isActive ? 600 : 500,
              color: isActive ? 'var(--primary)' : 'var(--fg-dim)',
              background: isActive ? 'var(--bg-secondary, var(--selected))' : 'transparent',
              borderBottom: isActive ? '2px solid var(--primary)' : '2px solid transparent',
              transition: 'color 0.15s, border-color 0.15s, background 0.15s',
            })}
          >
            <FontAwesomeIcon icon={tab.icon} style={{ fontSize: 11 }} />
            <span className="header-tab-label">{tab.label}</span>
          </NavLink>
        ))}
      </nav>

      <style>{`
        .header-nav a:hover:not([style*="var(--primary)"]) { color: var(--fg) !important; }
        @media (max-width: 768px) {
          .header-tab-label { display: none; }
          .header-nav a { padding: 0 0.5rem !important; }
        }
      `}</style>

      {/* Right actions */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
        {/* Connection status dot */}
        <div
          title={connected ? 'Connected' : 'Disconnected'}
          style={{
            width: 7,
            height: 7,
            borderRadius: '50%',
            background: connected ? 'var(--success)' : 'var(--error)',
            marginRight: '0.25rem',
          }}
        />

        {/* Theme toggle */}
        <button
          onClick={toggleTheme}
          title="Toggle theme"
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            padding: '0.25rem 0.375rem',
            borderRadius: 'var(--radius-sm)',
            display: 'flex',
            alignItems: 'center',
          }}
        >
          <FontAwesomeIcon icon={theme === 'light' ? faMoon : faSun} style={{ fontSize: 13 }} />
        </button>

        {/* Settings */}
        <NavLink
          to="/settings"
          title="Settings"
          style={({ isActive }) => ({
            display: 'flex',
            alignItems: 'center',
            color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
            padding: '0.25rem 0.375rem',
            borderRadius: 'var(--radius-sm)',
            background: isActive ? 'var(--selected)' : 'transparent',
          })}
        >
          <FontAwesomeIcon icon={faCog} style={{ fontSize: 13 }} />
        </NavLink>
      </div>
    </header>
  )
}
