import { useEffect, useState } from 'react'
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faBars } from '@fortawesome/free-solid-svg-icons'
import GeneralSettings from './GeneralSettings'
import AgentsSettings from './AgentsSettings'
import MCPServersSettings from './MCPServersSettings'
import MCPGatewaySettings from './MCPGatewaySettings'
import LSPSettings from './LSPSettings'
import InternalToolsSettings from './InternalToolsSettings'
import BashSettings from './BashSettings'
import TokenOptimizationSettings from './TokenOptimizationSettings'
import SkillsSettings from './SkillsSettings'
import LuaSettings from './LuaSettings'
import EvaluatorSettings from './EvaluatorSettings'
import MesnadaSettings from './MesnadaSettings'
import RemembrancesSettings from './RemembrancesSettings'
import SnapshotsSettings from './SnapshotsSettings'
import DesignSystemSettings from './DesignSystemSettings'
import APIServerSettings from './APIServerSettings'
import WebUIAccessSettings from './WebUIAccessSettings'
import ProviderAccountsSettings from './ProviderAccountsSettings'
import ContainerRuntimeSettings from './ContainerRuntimeSettings'
import { useConfigEventsStore } from '@pando/client/stores/configEventsStore'
import { useExtensionPanelsStore } from '@pando/client/stores/extensionPanelsStore'
import ExtensionSlot from '@/components/extensions/ExtensionSlot'

type SettingsCategory =
  | 'general'
  | 'providers'
  | 'agents'
  | 'mcp-servers'
  | 'mcp-gateway'
  | 'lsp'
  | 'tools'
  | 'bash'
  | 'token-optimization'
  | 'skills'
  | 'design-system'
  | 'lua'
  | 'self-improvement'
  | 'mesnada'
  | 'remembrances'
  | 'snapshots'
  | 'api-server'
  | 'webui-access'
  | 'container-runtime'

const CATEGORY_KEYS: { id: SettingsCategory; labelKey: string; group?: string }[] = [
  { id: 'general', labelKey: 'settings.categories.general' },
  { id: 'providers', labelKey: 'settings.categories.providers' },
  { id: 'agents', labelKey: 'settings.categories.agents' },
  { id: 'mcp-servers', labelKey: 'settings.categories.mcpServers' },
  { id: 'mcp-gateway', labelKey: 'settings.categories.mcpGateway' },
  { id: 'lsp', labelKey: 'settings.categories.lsp' },
  { id: 'tools', labelKey: 'settings.categories.tools' },
  { id: 'container-runtime', labelKey: 'settings.categories.containerRuntime' },
  { id: 'bash', labelKey: 'settings.categories.bash' },
  { id: 'token-optimization', labelKey: 'settings.categories.tokenOptimization' },
  { id: 'skills', labelKey: 'settings.categories.skills' },
  { id: 'design-system', labelKey: 'settings.categories.designSystem' },
  { id: 'lua', labelKey: 'settings.categories.lua' },
  { id: 'self-improvement', labelKey: 'settings.categories.selfImprovement' },
  { id: 'mesnada', labelKey: 'settings.categories.mesnada', group: 'services' },
  { id: 'remembrances', labelKey: 'settings.categories.remembrances', group: 'services' },
  { id: 'snapshots', labelKey: 'settings.categories.snapshots', group: 'services' },
  { id: 'api-server', labelKey: 'settings.categories.apiServer', group: 'services' },
  { id: 'webui-access', labelKey: 'settings.categories.webuiAccess', group: 'services' },
]

/**
 * A settings section contributed by an extension, addressed by its panel ID.
 * Prefixing keeps the extension namespace and the core one from ever colliding.
 */
type ExtensionCategory = `ext:${string}`
type ActiveCategory = SettingsCategory | ExtensionCategory

/** Below this width the view switches to a master/detail flow. */
const MOBILE_QUERY = '(max-width: 768px)'

export default function SettingsView() {
  const { t } = useTranslation()
  const [activeCategory, setActiveCategory] = useState<ActiveCategory>('general')
  const { connect, disconnect } = useConfigEventsStore()
  const extensionSections = useExtensionPanelsStore((s) => s.panels).filter((p) => p.slot === 'settings')

  // Track the mobile breakpoint so the category list and the section can take
  // turns owning the full width instead of splitting it.
  const [isMobile, setIsMobile] = useState(() => window.matchMedia(MOBILE_QUERY).matches)
  useEffect(() => {
    const mql = window.matchMedia(MOBILE_QUERY)
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  // Mobile only: the category list is the landing screen; picking a category
  // swaps it for the section, which offers a control to bring the list back.
  const [menuOpen, setMenuOpen] = useState(true)
  const showMenu = !isMobile || menuOpen
  const showContent = !isMobile || !menuOpen

  const selectCategory = (id: ActiveCategory) => {
    setActiveCategory(id)
    setMenuOpen(false)
  }

  // Connect to the config hot-reload SSE stream while this view is mounted.
  useEffect(() => {
    connect()
    return () => { disconnect() }
  }, [connect, disconnect])

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* Category list — mini sidebar on desktop, full-width menu on mobile */}
      {showMenu && (
      <nav
        style={{
          width: isMobile ? '100%' : 180,
          flexShrink: 0,
          background: 'var(--sidebar-bg)',
          borderRight: isMobile ? 'none' : '1px solid var(--border)',
          display: 'flex',
          flexDirection: 'column',
          padding: '1rem 0',
          overflowY: 'auto',
        }}
      >
        {CATEGORY_KEYS.filter((c) => !c.group).map((cat) => {
          const isActive = activeCategory === cat.id
          return (
            <button
              key={cat.id}
              onClick={() => selectCategory(cat.id)}
              style={{
                display: 'block',
                width: '100%',
                textAlign: 'left',
                padding: '0.5rem 1rem',
                background: isActive ? 'var(--selected)' : 'transparent',
                color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
                border: 'none',
                borderLeft: isActive
                  ? '3px solid var(--primary)'
                  : '3px solid transparent',
                fontSize: 14,
                fontWeight: isActive ? 600 : 400,
                cursor: 'pointer',
                transition: 'background 0.15s, color 0.15s',
                fontFamily: 'inherit',
              }}
              onMouseEnter={(e) => {
                if (!isActive) {
                  e.currentTarget.style.background = 'var(--hover)'
                  e.currentTarget.style.color = 'var(--fg)'
                }
              }}
              onMouseLeave={(e) => {
                if (!isActive) {
                  e.currentTarget.style.background = 'transparent'
                  e.currentTarget.style.color = 'var(--fg-muted)'
                }
              }}
            >
              {t(cat.labelKey)}
            </button>
          )
        })}

        {/* Services group */}
        <div
          style={{
            padding: '0.75rem 1rem 0.25rem',
            fontSize: 10,
            fontWeight: 700,
            color: 'var(--fg-dim)',
            textTransform: 'uppercase' as const,
            letterSpacing: '0.08em',
            borderTop: '1px solid var(--border)',
            marginTop: '0.5rem',
          }}
        >
          {t('nav.sections.services')}
        </div>
        {CATEGORY_KEYS.filter((c) => c.group === 'services').map((cat) => {
          const isActive = activeCategory === cat.id
          return (
            <button
              key={cat.id}
              onClick={() => selectCategory(cat.id)}
              style={{
                display: 'block',
                width: '100%',
                textAlign: 'left',
                padding: '0.5rem 1rem',
                background: isActive ? 'var(--selected)' : 'transparent',
                color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
                border: 'none',
                borderLeft: isActive
                  ? '3px solid var(--primary)'
                  : '3px solid transparent',
                fontSize: 14,
                fontWeight: isActive ? 600 : 400,
                cursor: 'pointer',
                transition: 'background 0.15s, color 0.15s',
                fontFamily: 'inherit',
              }}
              onMouseEnter={(e) => {
                if (!isActive) {
                  e.currentTarget.style.background = 'var(--hover)'
                  e.currentTarget.style.color = 'var(--fg)'
                }
              }}
              onMouseLeave={(e) => {
                if (!isActive) {
                  e.currentTarget.style.background = 'transparent'
                  e.currentTarget.style.color = 'var(--fg-muted)'
                }
              }}
            >
              {t(cat.labelKey)}
            </button>
          )
        })}
        {extensionSections.length > 0 && (
          <>
            <div
              style={{
                padding: '0.75rem 1rem 0.35rem',
                fontSize: 11,
                textTransform: 'uppercase',
                letterSpacing: '0.05em',
                color: 'var(--fg-dim)',
                borderTop: '1px solid var(--border)',
                marginTop: '0.5rem',
              }}
            >
              {t('settings.categories.extensions', 'Extensions')}
            </div>
            {extensionSections.map((panel) => {
              const id: ExtensionCategory = `ext:${panel.id}`
              const isActive = activeCategory === id
              return (
                <button
                  key={panel.id}
                  onClick={() => selectCategory(id)}
                  style={{
                    display: 'block',
                    width: '100%',
                    textAlign: 'left',
                    padding: '0.5rem 1rem',
                    background: isActive ? 'var(--selected)' : 'transparent',
                    color: isActive ? 'var(--primary)' : 'var(--fg-muted)',
                    border: 'none',
                    borderLeft: isActive ? '3px solid var(--primary)' : '3px solid transparent',
                    fontSize: 14,
                    fontWeight: isActive ? 600 : 400,
                    cursor: 'pointer',
                    fontFamily: 'inherit',
                  }}
                >
                  {panel.title || panel.id}
                </button>
              )
            })}
          </>
        )}
      </nav>
      )}

      {/* Content area */}
      {showContent && (
      <div
        style={{
          flex: 1,
          minWidth: 0,
          overflowY: 'auto',
          padding: isMobile ? '1rem' : '2rem',
          background: 'var(--bg)',
        }}
      >
        {isMobile && (
          <button onClick={() => setMenuOpen(true)} style={backButtonStyle}>
            <FontAwesomeIcon icon={faBars} style={{ fontSize: 12 }} />
            {t('settings.backToCategories')}
          </button>
        )}
        {activeCategory === 'general' && <GeneralSettings />}
        {activeCategory === 'providers' && <ProviderAccountsSettings />}
        {activeCategory === 'agents' && <AgentsSettings />}
        {activeCategory === 'mcp-servers' && <MCPServersSettings />}
        {activeCategory === 'mcp-gateway' && <MCPGatewaySettings />}
        {activeCategory === 'lsp' && <LSPSettings />}
        {activeCategory === 'tools' && <InternalToolsSettings />}
        {activeCategory === 'container-runtime' && <ContainerRuntimeSettings />}
        {activeCategory === 'bash' && <BashSettings />}
        {activeCategory === 'token-optimization' && <TokenOptimizationSettings />}
        {activeCategory === 'skills' && <SkillsSettings />}
        {activeCategory === 'lua' && <LuaSettings />}
        {activeCategory === 'self-improvement' && <EvaluatorSettings />}
        {activeCategory === 'mesnada' && <MesnadaSettings />}
        {activeCategory === 'remembrances' && <RemembrancesSettings />}
        {activeCategory === 'snapshots' && <SnapshotsSettings />}
        {activeCategory === 'design-system' && <DesignSystemSettings />}
        {activeCategory === 'api-server' && <APIServerSettings />}
        {activeCategory === 'webui-access' && <WebUIAccessSettings />}
        {activeCategory.startsWith('ext:') && (
          <ExtensionSlot slot="settings" panelId={activeCategory.slice('ext:'.length)} />
        )}
      </div>
      )}
    </div>
  )
}

const backButtonStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.5rem',
  marginBottom: '1rem',
  padding: '0.4rem 0.7rem',
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg-muted)',
  fontSize: 13,
  fontFamily: 'inherit',
  cursor: 'pointer',
}
