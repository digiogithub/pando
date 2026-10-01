import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useTheme } from '@/hooks/useTheme'
import { useAgentBusy } from '@/hooks/useAgentBusy'
import { BrandMark } from '@/components/brand'
import PersonaSelector from '@/components/shared/PersonaSelector'
import { IconButton, Tooltip } from '@/components/ui'
import { isProjectChildMode } from '@pando/client/services/api'
import {
  CircleQuestionMark, LayoutDashboard, MessageSquare, Moon, PanelLeft, PanelLeftClose, Settings, Sun,
} from '@/components/ui/icons'
import { isMacPlatform } from './shellHooks'
import DesktopWindowControls from './DesktopWindowControls'
import { onTitleBarDoubleClick, useDesktopShell } from '@/services/desktopWindow'
import { resolveHeaderSection } from './headerSections'

const DOCS_URL = 'https://madeindigio.github.io/pando-docs/'

/**
 * App title bar. In the simple chat mode (`simple`) it keeps the same chrome
 * (sidebar toggle, brand, title, window controls) but drops every advanced
 * action: only the way back to the full view and the theme toggle remain;
 * settings live in the sidebar.
 */
export default function Header({ isMobile = false, simple = false }: { isMobile?: boolean; simple?: boolean }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const { toggleSidebar, sidebarOpen, setChatMode } = useLayoutStore()
  const { resolvedMode, toggleMode } = useTheme()
  const version = useServerStore((s) => s.version)
  const startupMode = useServerStore((s) => s.startupMode)
  const projectName = useServerStore((s) => s.projectName)
  const busy = useAgentBusy()
  const activeSession = useSessionStore((s) => s.sessions.find((x) => x.id === s.activeSessionId))
  const extensionPanels = useExtensionPanelsStore((s) => s.panels)
  const activeTabId = useProjectTabsStore((s) => s.activeTabId)
  const activeProjectTab = useProjectTabsStore((s) =>
    s.activeTabId === 'main'
      ? null
      : s.tabs.find((tab) => tab.projectId === s.activeTabId) ?? null,
  )
  const desktopShell = useDesktopShell()
  const childMode = startupMode === 'project-child' || isProjectChildMode()

  // Title: section name, plus the active session on chat routes.
  const section = resolveHeaderSection(location.pathname, (key) => t(key), extensionPanels)
  const first = location.pathname.split('/').filter(Boolean)[0] ?? ''
  const isChatRoute = first === '' || first === 'chat'
  const sessionTitle = isChatRoute && activeSession
    ? activeSession.title || t('nav.untitledSession')
    : ''
  const cleanVersion = version && version !== 'unknown'
    ? version.replace(/\+.*$/, '')
    : ''
  const versionLabel = cleanVersion ? (cleanVersion.startsWith('v') ? cleanVersion : `v${cleanVersion}`) : ''

  const shortcutMod = isMacPlatform ? '⌘' : 'Ctrl'
  const themeLabel = resolvedMode === 'dark'
    ? t('shell.switchToLight', 'Switch to light mode')
    : t('shell.switchToDark', 'Switch to dark mode')
  const themeTooltip = `${themeLabel} (${shortcutMod}+Shift+L)`

  const sidebarLabel = isMobile
    ? t('shell.openMenu', 'Open menu')
    : sidebarOpen
      ? t('shell.collapseSidebar', 'Collapse sidebar')
      : t('shell.expandSidebar', 'Expand sidebar')
  const projectTabSection = activeProjectTab?.name ?? ''
  const projectTabDetail = activeProjectTab?.childTitle ?? section
  const titleSection = childMode && projectName
    ? projectName
    : activeTabId !== 'main' && projectTabSection
      ? projectTabSection
      : section
  const titleDetail = childMode && projectName
    ? (sessionTitle || section)
    : activeTabId !== 'main'
      ? projectTabDetail
      : sessionTitle

  return (
    <header className="shell-titlebar" onDoubleClick={desktopShell ? onTitleBarDoubleClick : undefined}>
      <div className="shell-titlebar-group">
        <IconButton
          aria-label={sidebarLabel}
          tooltip={`${sidebarLabel} (Ctrl+B)`}
          icon={!isMobile && sidebarOpen ? <PanelLeftClose /> : <PanelLeft />}
          onClick={toggleSidebar}
        />
        {/* Version lives in the tooltip only: the title bar stays quiet. */}
        <div className="shell-brand" aria-label="Pando" title={versionLabel ? `Pando ${versionLabel}` : 'Pando'}>
          <span className="shell-brand-glyph"><BrandMark size={18} pulse={busy} /></span>
          <span className="shell-brand-name">Pando</span>
        </div>
      </div>

      <div className="shell-title" aria-live="polite">
        {titleDetail ? (
          <>
              <span className="shell-title-section">{titleSection}</span>
              <span className="shell-title-sep" aria-hidden="true">/</span>
              <span className="shell-title-text shell-title-strong">{titleDetail}</span>
          </>
        ) : (
          titleSection && <span className="shell-title-text">{titleSection}</span>
        )}
      </div>

      <div className="shell-titlebar-actions">
        {simple ? (
          <Tooltip content={t('header.fullViewHint', 'Switch back to the full view')}>
            <button
              type="button"
              className="ui-btn ui-btn--ghost ui-btn--sm shell-fullview-btn"
              aria-label={t('header.fullViewHint', 'Switch back to the full view')}
              onClick={() => { setChatMode('advanced'); navigate('/') }}
            >
              <LayoutDashboard size={16} aria-hidden="true" />
              <span className="shell-hide-mobile">{t('header.fullView', 'Full view')}</span>
            </button>
          </Tooltip>
        ) : (
          <>
          <span className="shell-hide-mobile">
            <PersonaSelector />
          </span>
          <span className="shell-titlebar-divider shell-hide-mobile" aria-hidden="true" />
          <IconButton
            className="shell-hide-mobile"
            aria-label={t('header.simpleChat', 'Simple Chat')}
            tooltip
            icon={<MessageSquare />}
            onClick={() => { setChatMode('simple'); navigate('/chat/simple') }}
          />
          <Tooltip content={t('shell.documentation', 'Documentation')}>
            <a
              className="ui-btn ui-btn--ghost ui-btn--icon shell-hide-mobile"
              href={DOCS_URL}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={t('shell.documentation', 'Documentation')}
            >
              <CircleQuestionMark />
            </a>
          </Tooltip>
          </>
        )}
        <IconButton
          aria-label={themeLabel}
          tooltip={themeTooltip}
          icon={resolvedMode === 'dark' ? <Sun /> : <Moon />}
          onClick={toggleMode}
        />
        {!simple && (
          <Tooltip content={t('nav.settings')}>
            <NavLink to="/settings" className="shell-icon-link" aria-label={t('nav.settings')}>
              <Settings />
            </NavLink>
          </Tooltip>
        )}
        <DesktopWindowControls />
      </div>
    </header>
  )
}
