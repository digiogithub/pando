import { useEffect, useState } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import { useTheme } from '@/hooks/useTheme'
import { useAnimatedLogo } from '@/hooks/useAnimatedLogo'
import PersonaSelector from '@/components/shared/PersonaSelector'
import { IconButton, Tooltip } from '@/components/ui'
import {
  CircleQuestionMark, MessageSquare, Moon, PanelLeft, PanelLeftClose, Settings, Sun,
} from '@/components/ui/icons'
import { isMacPlatform } from './shellHooks'

const DOCS_URL = 'https://madeindigio.github.io/pando-docs/'

/** i18n key of the section shown in the title bar, by first path segment. */
const SECTION_KEYS: Record<string, string> = {
  '': 'nav.chat',
  chat: 'nav.chat',
  projects: 'nav.projects',
  orchestrator: 'nav.orchestrator',
  evaluator: 'nav.selfImprovement',
  snapshots: 'nav.agentVcs',
  logs: 'nav.logs',
  editor: 'nav.codeEditor',
  terminal: 'nav.terminal',
  settings: 'nav.settings',
  design: 'nav.design',
  instances: 'nav.instances',
}

export default function Header({ isMobile = false }: { isMobile?: boolean }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const { toggleSidebar, sidebarOpen, setChatMode } = useLayoutStore()
  const { resolvedMode, toggleMode } = useTheme()
  const [version, setVersion] = useState<string>('')
  const logoGlyph = useAnimatedLogo()
  const activeSession = useSessionStore((s) => s.sessions.find((x) => x.id === s.activeSessionId))
  const extensionPanels = useExtensionPanelsStore((s) => s.panels)

  useEffect(() => {
    fetch('/health')
      .then((r) => r.json())
      .then((d) => {
        if (d.version && d.version !== 'unknown') {
          const clean = d.version.replace(/\+.*$/, '')
          setVersion(clean.startsWith('v') ? clean : `v${clean}`)
        }
      })
      .catch(() => {})
  }, [])

  // Title: section name, plus the active session on chat routes.
  const segments = location.pathname.split('/').filter(Boolean)
  const first = segments[0] ?? ''
  let section = SECTION_KEYS[first] ? t(SECTION_KEYS[first]) : ''
  if (first === 'ext' && segments[1]) {
    const panel = extensionPanels.find((p) => p.id === segments[1])
    section = panel?.title || segments[1]
  }
  const isChatRoute = first === '' || first === 'chat'
  const sessionTitle = isChatRoute && activeSession
    ? activeSession.title || t('nav.untitledSession')
    : ''

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

  return (
    <header className="shell-titlebar">
      <div className="shell-titlebar-group">
        <IconButton
          aria-label={sidebarLabel}
          tooltip={`${sidebarLabel} (Ctrl+B)`}
          icon={!isMobile && sidebarOpen ? <PanelLeftClose /> : <PanelLeft />}
          onClick={toggleSidebar}
        />
        {/* Version lives in the tooltip only: the title bar stays quiet. */}
        <div className="shell-brand" aria-label="Pando" title={version ? `Pando ${version}` : 'Pando'}>
          <span className="shell-brand-glyph" aria-hidden="true">{logoGlyph}</span>
          <span className="shell-brand-name">Pando</span>
        </div>
      </div>

      <div className="shell-title" aria-live="polite">
        {sessionTitle ? (
          <>
            <span className="shell-title-section">{section}</span>
            <span className="shell-title-sep" aria-hidden="true">/</span>
            <span className="shell-title-text shell-title-strong">{sessionTitle}</span>
          </>
        ) : (
          section && <span className="shell-title-text">{section}</span>
        )}
      </div>

      <div className="shell-titlebar-actions">
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
        <IconButton
          aria-label={themeLabel}
          tooltip={themeTooltip}
          icon={resolvedMode === 'dark' ? <Sun /> : <Moon />}
          onClick={toggleMode}
        />
        <Tooltip content={t('nav.settings')}>
          <NavLink to="/settings" className="shell-icon-link" aria-label={t('nav.settings')}>
            <Settings />
          </NavLink>
        </Tooltip>
      </div>
    </header>
  )
}
