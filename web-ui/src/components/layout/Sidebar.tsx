import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { format } from 'date-fns'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import { IconButton, Input, Tooltip } from '@/components/ui'
import { BrandMark } from '@/components/brand'
import {
  ChevronDown, Code, FolderOpen, GitBranch, type LucideIcon, MessageSquare, MessageSquarePlus,
  Network, Palette, Puzzle, ScrollText, Search, Server, Settings, Sparkles, SquarePen,
  SquareTerminal, X,
} from '@/components/ui/icons'
import { isMobileViewport } from './shellHooks'

export type SidebarVariant = 'full' | 'rail' | 'drawer'

const SECTIONS_KEY = 'pando_sidebar_sections'

interface SectionState { nav: boolean; recents: boolean }

function readSections(): SectionState {
  try {
    const raw = localStorage.getItem(SECTIONS_KEY)
    if (raw) return { nav: true, recents: true, ...(JSON.parse(raw) as Partial<SectionState>) }
  } catch {
    // ignore unreadable storage
  }
  return { nav: true, recents: true }
}

function writeSections(s: SectionState) {
  try {
    localStorage.setItem(SECTIONS_KEY, JSON.stringify(s))
  } catch {
    // ignore
  }
}

interface NavItem { path: string; label: string; icon: LucideIcon; end?: boolean }

export default function Sidebar({ variant = 'full' }: { variant?: SidebarVariant }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const [sections, setSections] = useState<SectionState>(readSections)
  const [query, setQuery] = useState('')
  const {
    sessions,
    activeSessionId,
    setActiveSession,
    setMessages,
    sessionsHasMore,
    sessionsLoadingMore,
    sessionsTotal,
    loadMoreSessions,
  } = useSessionStore()
  const setSidebarOpen = useLayoutStore((s) => s.setSidebarOpen)
  const sessionsLoading = useSessionStore((s) => s.loading)
  const extensionPanels = useExtensionPanelsStore((s) => s.panels)

  const rail = variant === 'rail'

  const toggleSection = (key: keyof SectionState) => {
    setSections((prev) => {
      const next = { ...prev, [key]: !prev[key] }
      writeSections(next)
      return next
    })
  }

  const closeSidebarOnMobile = () => {
    if (isMobileViewport()) {
      setSidebarOpen(false)
    }
  }

  const newSession = () => {
    useSessionStore.setState({ activeSessionId: null })
    setMessages([])
    const p = location.pathname
    if (p !== '/' && p !== '/chat') navigate('/')
    closeSidebarOnMobile()
  }

  const NAV_ITEMS: NavItem[] = [
    { path: '/', label: t('nav.chat'), icon: MessageSquare, end: true },
    { path: '/chat/simple', label: t('nav.simpleChat'), icon: MessageSquarePlus },
    { path: '/projects', label: t('nav.projects'), icon: FolderOpen },
    { path: '/editor', label: t('nav.codeEditor'), icon: Code },
    { path: '/terminal', label: t('nav.terminal'), icon: SquareTerminal },
    { path: '/orchestrator', label: t('nav.orchestrator'), icon: Network },
    { path: '/design', label: t('nav.design'), icon: Palette },
    { path: '/logs', label: t('nav.logs'), icon: ScrollText },
    { path: '/snapshots', label: t('nav.agentVcs'), icon: GitBranch },
    { path: '/evaluator', label: t('nav.selfImprovement'), icon: Sparkles },
    { path: '/instances', label: t('nav.instances'), icon: Server },
  ]

  // Sidebar panels contributed by compiled-in extensions. They are appended
  // rather than merged into NAV_ITEMS above so a build can never reorder or
  // displace a core entry. Their labels come from the extension, not from
  // i18n: core has no translations for a panel it does not know about.
  const EXT_ITEMS: NavItem[] = extensionPanels
    .filter((p) => p.slot === 'sidebar')
    .map((p) => ({ path: `/ext/${p.id}`, label: p.title || p.id, icon: Puzzle }))

  const filteredSessions = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return sessions
    return sessions.filter((s) =>
      (s.title || '').toLowerCase().includes(q) || (s.prompt_preview || '').toLowerCase().includes(q),
    )
  }, [sessions, query])

  const renderNavItem = (item: NavItem) => {
    const Icon = item.icon
    // Orchestrator = Mesnada agent orchestration: its own small identity
    // instead of the generic lucide icon, in both the full nav and the rail.
    const iconNode =
      item.path === '/orchestrator' ? (
        <BrandMark variant="mesnada" size={rail ? 18 : 16} />
      ) : (
        <Icon size={rail ? 18 : 16} />
      )
    const link = (
      <NavLink
        key={item.path}
        to={item.path}
        end={item.end}
        onClick={closeSidebarOnMobile}
        className="shell-nav-item"
        aria-label={rail ? item.label : undefined}
      >
        {iconNode}
        {!rail && <span className="shell-nav-label">{item.label}</span>}
      </NavLink>
    )
    if (!rail) return link
    return (
      <Tooltip key={item.path} content={item.label} placement="right">
        {link}
      </Tooltip>
    )
  }

  const settingsItem: NavItem = { path: '/settings', label: t('nav.settings'), icon: Settings }
  const recentsOpen = sections.recents || query.trim() !== ''

  return (
    <aside
      className={`shell-sidebar${rail ? ' is-rail' : ''}${variant === 'drawer' ? ' is-drawer' : ''}`}
      aria-label={t('shell.sidebar', 'Sidebar')}
    >
      {variant === 'drawer' && (
        <div className="shell-drawer-head">
          <span className="shell-brand">
            <span className="shell-brand-name">Pando</span>
          </span>
          <IconButton
            aria-label={t('shell.closeMenu', 'Close menu')}
            icon={<X />}
            onClick={() => setSidebarOpen(false)}
          />
        </div>
      )}

      <div className="shell-sidebar-top">
        {rail ? (
          <IconButton
            aria-label={t('nav.newSession')}
            tooltip={<>{t('nav.newSession')}</>}
            icon={<SquarePen />}
            variant="ghost"
            onClick={newSession}
          />
        ) : (
          <>
            <button type="button" className="shell-newchat" onClick={newSession}>
              <SquarePen size={15} className="shell-newchat-icon" aria-hidden="true" />
              {t('nav.newSession')}
            </button>
            <div className="shell-search">
              <Search size={14} className="shell-search-icon" aria-hidden="true" />
              <Input
                size="sm"
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t('shell.searchSessions', 'Search sessions')}
                aria-label={t('shell.searchSessions', 'Search sessions')}
              />
            </div>
          </>
        )}
      </div>

      <div className="shell-sidebar-scroll">
        {rail ? (
          <div className="shell-section">
            {NAV_ITEMS.map(renderNavItem)}
            {EXT_ITEMS.map(renderNavItem)}
          </div>
        ) : (
          <>
            <Section
              label={t('nav.sections.navigate')}
              open={sections.nav}
              onToggle={() => toggleSection('nav')}
            >
              {NAV_ITEMS.map(renderNavItem)}
              {EXT_ITEMS.map(renderNavItem)}
            </Section>

            <Section
              label={t('nav.sections.sessions')}
              open={recentsOpen}
              onToggle={() => toggleSection('recents')}
              count={sessionsTotal > 0 ? sessionsTotal : undefined}
            >
              {filteredSessions.map((s) => {
                const active = s.id === activeSessionId
                return (
                  <button
                    key={s.id}
                    type="button"
                    className="shell-session"
                    aria-current={active ? 'true' : undefined}
                    title={s.prompt_preview || s.title || t('nav.untitledSession')}
                    onClick={() => {
                      setActiveSession(s.id)
                      closeSidebarOnMobile()
                    }}
                  >
                    <span
                      className={`shell-dot${s.is_running ? ' is-running' : active ? ' is-active' : ''}`}
                      aria-hidden="true"
                    />
                    <span className="shell-session-main">
                      <span className="shell-session-title">{s.title || t('nav.untitledSession')}</span>
                      <span className={`shell-session-meta${s.is_running ? ' is-running' : ''}`}>
                        {s.is_running
                          ? t('shell.running', 'Running')
                          : <>{s.message_count} {t('common.messages')} · {format(new Date(s.updated_at), 'MMM d')}</>}
                      </span>
                    </span>
                  </button>
                )
              })}
              {sessionsHasMore && !query && (
                <button
                  type="button"
                  className="shell-loadmore"
                  onClick={() => void loadMoreSessions()}
                  disabled={sessionsLoadingMore}
                >
                  {sessionsLoadingMore
                    ? t('common.loading')
                    : `${t('common.loadMore')} (${sessions.length}/${sessionsTotal})`}
                </button>
              )}
              {sessions.length === 0 && (
                <div className="shell-sidebar-note">{sessionsLoading ? t('common.loading') : t('nav.noSessionsYet')}</div>
              )}
              {sessions.length > 0 && filteredSessions.length === 0 && (
                <div className="shell-sidebar-note">{t('shell.noMatches', 'No matching sessions')}</div>
              )}
            </Section>
          </>
        )}
      </div>

      <div className="shell-sidebar-bottom">
        {renderNavItem(settingsItem)}
      </div>
    </aside>
  )
}

function Section({
  label, open, onToggle, count, children,
}: {
  label: string
  open: boolean
  onToggle: () => void
  count?: number
  children: ReactNode
}) {
  return (
    <div className="shell-section">
      <button type="button" className="shell-section-header" aria-expanded={open} onClick={onToggle}>
        <span>{label}</span>
        <ChevronDown size={12} />
        {count !== undefined && <span className="shell-section-count">{count}</span>}
      </button>
      {open && <div className="shell-section-body">{children}</div>}
    </div>
  )
}
