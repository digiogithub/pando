import { NavLink } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faComments, faPuzzlePiece, faFileLines, faNetworkWired,
  faCamera, faStar, faCog, faMoon, faSun,
  faChevronLeft, faChevronRight,
} from '@fortawesome/free-solid-svg-icons'
import { useLayoutStore } from '@/stores/layoutStore'
import { useTheme } from '@/hooks/useTheme'
import { useServerStore } from '@/stores/serverStore'

const TABS = [
  { path: '/', label: 'Chat', icon: faComments, end: true },
  { path: '/orchestrator', label: 'Add-ons', icon: faPuzzlePiece },
  { path: '/logs', label: 'Logs', icon: faFileLines },
  { path: '/orchestrator', label: 'Orchestrator', icon: faNetworkWired },
  { path: '/snapshots', label: 'Snapshots', icon: faCamera },
  { path: '/evaluator', label: 'Evaluator', icon: faStar },
]

// CSS filter for gold color on the favicon SVG (approximates --primary amber/gold)
const FAVICON_GOLD_FILTER =
  'brightness(0) saturate(100%) invert(65%) sepia(60%) saturate(500%) hue-rotate(5deg) brightness(92%)'

export default function Header() {
  const { toggleSidebar, sidebarOpen } = useLayoutStore()
  const { theme, toggleTheme } = useTheme()
  const connected = useServerStore((s) => s.connected)

  return (
    <header
      style={{
        display: 'flex',
        alignItems: 'center',
        height: 48,
        borderBottom: '1px solid var(--border)',
        background: 'var(--sidebar-bg)',
        paddingLeft: '0.5rem',
        paddingRight: '1rem',
        gap: '0.25rem',
        flexShrink: 0,
        zIndex: 10,
      }}
    >
      {/* Botón flecha para replegar sidebar */}
      <button
        onClick={toggleSidebar}
        title="Toggle sidebar (Ctrl+B)"
        style={{
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--fg-muted)',
          padding: '0.25rem 0.5rem',
          borderRadius: 'var(--radius-sm)',
          display: 'flex',
          alignItems: 'center',
          flexShrink: 0,
        }}
      >
        <FontAwesomeIcon
          icon={sidebarOpen ? faChevronLeft : faChevronRight}
          style={{ fontSize: 13 }}
        />
      </button>

      {/* Favicon en color dorado */}
      <img
        src="/pando-favicon.svg"
        alt="Pando"
        style={{ width: 22, height: 22, filter: FAVICON_GOLD_FILTER, flexShrink: 0 }}
      />

      {/* Nombre de la app */}
      <span
        style={{
          fontWeight: 700,
          fontSize: 15,
          color: 'var(--primary)',
          letterSpacing: '-0.01em',
          marginRight: '0.5rem',
          flexShrink: 0,
        }}
      >
        Pando
      </span>

      {/* Tab navigation */}
      <nav style={{ display: 'flex', flex: 1, gap: 2 }} className="header-nav">
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
              padding: '0.25rem 0.75rem',
              borderRadius: 'var(--radius-sm)',
              textDecoration: 'none',
              fontSize: 13,
              fontWeight: isActive ? 600 : 400,
              color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
              borderBottom: isActive ? '2px solid var(--primary)' : '2px solid transparent',
              background: isActive ? 'var(--selected)' : 'transparent',
              transition: 'all 0.15s',
            })}
          >
            <FontAwesomeIcon icon={tab.icon} style={{ fontSize: 11 }} />
            <span className="header-tab-label">{tab.label}</span>
          </NavLink>
        ))}
      </nav>
      <style>{`
        @media (max-width: 768px) {
          .header-tab-label { display: none; }
          .header-nav a { padding: 0.25rem 0.5rem !important; }
        }
        @media (max-width: 480px) {
          .header-nav { gap: 0 !important; }
        }
      `}</style>

      {/* Right actions */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
        {/* Connection status */}
        <div
          title={connected ? 'Connected' : 'Disconnected'}
          style={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: connected ? 'var(--success)' : 'var(--error)',
          }}
        />

        {/* Theme toggle */}
        <button
          onClick={toggleTheme}
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            padding: '0.25rem 0.5rem',
            borderRadius: 'var(--radius-sm)',
            display: 'flex',
            alignItems: 'center',
          }}
          title="Toggle theme"
        >
          <FontAwesomeIcon icon={theme === 'light' ? faMoon : faSun} style={{ fontSize: 14 }} />
        </button>

        {/* Settings */}
        <NavLink
          to="/settings"
          style={({ isActive }) => ({
            display: 'flex',
            alignItems: 'center',
            color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
            padding: '0.25rem 0.5rem',
            borderRadius: 'var(--radius-sm)',
            background: isActive ? 'var(--selected)' : 'transparent',
          })}
          title="Settings"
        >
          <FontAwesomeIcon icon={faCog} style={{ fontSize: 14 }} />
        </NavLink>
      </div>
    </header>
  )
}
