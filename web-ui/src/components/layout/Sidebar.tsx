import { NavLink } from 'react-router-dom'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faComments, faPlus, faNetworkWired, faFileLines, faCamera,
  faStar, faCode, faTerminal, faCog, faChevronDown, faChevronRight,
  faCircle, faFolderOpen
} from '@fortawesome/free-solid-svg-icons'
import { useSessionStore } from '@/stores/sessionStore'
import { useLayoutStore } from '@/stores/layoutStore'
import { format } from 'date-fns'

export default function Sidebar() {
  const { t } = useTranslation()
  const [sessionsOpen, setSessionsOpen] = useState(true)
  const [navOpen, setNavOpen] = useState(true)
  const { sessions, activeSessionId, setActiveSession, setMessages } = useSessionStore()
  const setSidebarOpen = useLayoutStore((s) => s.setSidebarOpen)

  const closeSidebarOnMobile = () => {
    if (window.matchMedia('(max-width: 768px)').matches) {
      setSidebarOpen(false)
    }
  }

  const NAV_ITEMS = [
    { path: '/', label: t('nav.chat'), icon: faComments, end: true },
    { path: '/chat/simple', label: t('nav.simpleChat'), icon: faComments },
    { path: '/orchestrator', label: t('nav.orchestrator'), icon: faNetworkWired },
    { path: '/logs', label: t('nav.logs'), icon: faFileLines },
    { path: '/snapshots', label: t('nav.snapshots'), icon: faCamera },
    { path: '/evaluator', label: t('nav.selfImprovement'), icon: faStar },
    { path: '/editor', label: t('nav.codeEditor'), icon: faCode },
    { path: '/terminal', label: t('nav.terminal'), icon: faTerminal },
    { path: '/settings', label: t('nav.settings'), icon: faCog },
    { path: '/projects', label: t('nav.projects'), icon: faFolderOpen },
  ]

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
          label={t('nav.sections.sessions')}
          open={sessionsOpen}
          onToggle={() => setSessionsOpen(!sessionsOpen)}
          action={
            <button
              title={t('nav.newSession')}
              onClick={() => {
                useSessionStore.setState({ activeSessionId: null })
                setMessages([])
                closeSidebarOnMobile()
              }}
              style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--fg-muted)', padding: 0, lineHeight: 1 }}
            >
              <FontAwesomeIcon icon={faPlus} style={{ fontSize: 11 }} />
            </button>
          }
        />
        {sessionsOpen && (
          <div style={{ paddingBottom: '0.25rem' }}>
            {sessions.slice(0, 20).map((s) => (
              <button
                key={s.id}
                onClick={() => {
                  setActiveSession(s.id)
                  closeSidebarOnMobile()
                }}
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
                    {s.title || t('nav.untitledSession')}
                  </div>
                  <div style={{ fontSize: 10, color: 'var(--fg-muted)' }}>
                    {s.message_count} {t('common.messages')} · {format(new Date(s.updated_at), 'MMM d')}
                  </div>
                </div>
              </button>
            ))}
            {sessions.length === 0 && (
              <div style={{ padding: '0.5rem 1rem', fontSize: 12, color: 'var(--fg-dim)' }}>
                {t('nav.noSessionsYet')}
              </div>
            )}
          </div>
        )}

        {/* Navigation section */}
        <SectionHeader
          label={t('nav.sections.navigate')}
          open={navOpen}
          onToggle={() => setNavOpen(!navOpen)}
        />
        {navOpen && (
          <div style={{ paddingBottom: '0.25rem' }}>
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.path}
                to={item.path}
                end={item.end}
                onClick={closeSidebarOnMobile}
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
