import { NavLink } from 'react-router-dom'
import { useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faComments, faPlus, faNetworkWired, faFileLines, faCamera,
  faStar, faCode, faTerminal, faCog, faChevronDown, faChevronRight,
  faCircle
} from '@fortawesome/free-solid-svg-icons'
import { useSessionStore } from '@/stores/sessionStore'
import { format } from 'date-fns'

const NAV_ITEMS = [
  { path: '/', label: 'Chat', icon: faComments, end: true },
  { path: '/chat/simple', label: 'Simple Chat', icon: faComments },
  { path: '/orchestrator', label: 'Orchestrator', icon: faNetworkWired },
  { path: '/logs', label: 'Logs', icon: faFileLines },
  { path: '/snapshots', label: 'Snapshots', icon: faCamera },
  { path: '/evaluator', label: 'Evaluator', icon: faStar },
  { path: '/editor', label: 'Code Editor', icon: faCode },
  { path: '/terminal', label: 'Terminal', icon: faTerminal },
  { path: '/settings', label: 'Settings', icon: faCog },
]

export default function Sidebar() {
  const [sessionsOpen, setSessionsOpen] = useState(true)
  const [navOpen, setNavOpen] = useState(true)
  const { sessions, activeSessionId, setActiveSession } = useSessionStore()

  return (
    <aside
      style={{
        width: 260,
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--sidebar-bg)',
        borderRight: '1px solid var(--border)',
        overflow: 'hidden',
      }}
    >
      {/* Scrollable content */}
      <div style={{ flex: 1, overflow: 'auto', padding: '0.5rem 0' }}>

        {/* Sessions section */}
        <SectionHeader
          label="Sessions"
          open={sessionsOpen}
          onToggle={() => setSessionsOpen(!sessionsOpen)}
          action={
            <NavLink to="/" title="New session" style={{ color: 'var(--fg-muted)' }}>
              <FontAwesomeIcon icon={faPlus} style={{ fontSize: 11 }} />
            </NavLink>
          }
        />
        {sessionsOpen && (
          <div style={{ paddingBottom: '0.25rem' }}>
            {sessions.slice(0, 20).map((s) => (
              <button
                key={s.id}
                onClick={() => setActiveSession(s.id)}
                style={{
                  width: 'calc(100% - 1rem)',
                  background: s.id === activeSessionId ? 'var(--selected)' : 'transparent',
                  border: 'none',
                  borderRadius: 'var(--radius-sm)',
                  margin: '1px 0.5rem',
                  padding: '0.375rem 0.5rem',
                  cursor: 'pointer',
                  textAlign: 'left',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.5rem',
                }}
              >
                <FontAwesomeIcon
                  icon={faCircle}
                  style={{ fontSize: 6, color: s.id === activeSessionId ? 'var(--primary)' : 'var(--fg-dim)', flexShrink: 0 }}
                />
                <div style={{ flex: 1, overflow: 'hidden' }}>
                  <div style={{ fontSize: 12, fontWeight: 500, color: 'var(--fg)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {s.title || 'Untitled session'}
                  </div>
                  <div style={{ fontSize: 10, color: 'var(--fg-muted)' }}>
                    {s.message_count} msgs · {format(new Date(s.updated_at), 'MMM d')}
                  </div>
                </div>
              </button>
            ))}
            {sessions.length === 0 && (
              <div style={{ padding: '0.5rem 1rem', fontSize: 12, color: 'var(--fg-dim)' }}>
                No sessions yet
              </div>
            )}
          </div>
        )}

        {/* Navigation section */}
        <SectionHeader
          label="Navigate"
          open={navOpen}
          onToggle={() => setNavOpen(!navOpen)}
        />
        {navOpen && (
          <div style={{ paddingBottom: '0.25rem' }}>
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.label}
                to={item.path}
                end={item.end}
                style={({ isActive }) => ({
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.625rem',
                  padding: '0.375rem 0.75rem',
                  margin: '1px 0.5rem',
                  borderRadius: 'var(--radius-sm)',
                  textDecoration: 'none',
                  fontSize: 13,
                  color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
                  background: isActive ? 'var(--selected)' : 'transparent',
                  fontWeight: isActive ? 600 : 400,
                })}
              >
                <FontAwesomeIcon icon={item.icon} style={{ fontSize: 12, width: 14 }} />
                {item.label}
              </NavLink>
            ))}
          </div>
        )}
      </div>
    </aside>
  )
}

function SectionHeader({
  label, open, onToggle, action
}: {
  label: string
  open: boolean
  onToggle: () => void
  action?: React.ReactNode
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        padding: '0.375rem 0.75rem',
        cursor: 'pointer',
        userSelect: 'none',
      }}
      onClick={onToggle}
    >
      <FontAwesomeIcon
        icon={open ? faChevronDown : faChevronRight}
        style={{ fontSize: 9, color: 'var(--fg-dim)', marginRight: '0.375rem' }}
      />
      <span style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-dim)', textTransform: 'uppercase', letterSpacing: '0.05em', flex: 1 }}>
        {label}
      </span>
      {action && <span onClick={(e) => e.stopPropagation()}>{action}</span>}
    </div>
  )
}
