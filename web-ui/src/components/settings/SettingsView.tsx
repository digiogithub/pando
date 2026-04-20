import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import GeneralSettings from './GeneralSettings'
import ProvidersSettings from './ProvidersSettings'
import AgentsSettings from './AgentsSettings'
import MCPServersSettings from './MCPServersSettings'
import MCPGatewaySettings from './MCPGatewaySettings'
import LSPSettings from './LSPSettings'
import InternalToolsSettings from './InternalToolsSettings'
import BashSettings from './BashSettings'
import SkillsSettings from './SkillsSettings'
import LuaSettings from './LuaSettings'
import EvaluatorSettings from './EvaluatorSettings'
import MesnadaSettings from './MesnadaSettings'
import RemembrancesSettings from './RemembrancesSettings'
import SnapshotsSettings from './SnapshotsSettings'
import APIServerSettings from './APIServerSettings'
import { useConfigEventsStore } from '@/stores/configEventsStore'

type SettingsCategory =
  | 'general'
  | 'providers'
  | 'agents'
  | 'mcp-servers'
  | 'mcp-gateway'
  | 'lsp'
  | 'tools'
  | 'bash'
  | 'skills'
  | 'lua'
  | 'self-improvement'
  | 'mesnada'
  | 'remembrances'
  | 'snapshots'
  | 'api-server'

const CATEGORY_KEYS: { id: SettingsCategory; labelKey: string; group?: string }[] = [
  { id: 'general', labelKey: 'settings.categories.general' },
  { id: 'providers', labelKey: 'settings.categories.providers' },
  { id: 'agents', labelKey: 'settings.categories.agents' },
  { id: 'mcp-servers', labelKey: 'settings.categories.mcpServers' },
  { id: 'mcp-gateway', labelKey: 'settings.categories.mcpGateway' },
  { id: 'lsp', labelKey: 'settings.categories.lsp' },
  { id: 'tools', labelKey: 'settings.categories.tools' },
  { id: 'bash', labelKey: 'settings.categories.bash' },
  { id: 'skills', labelKey: 'settings.categories.skills' },
  { id: 'lua', labelKey: 'settings.categories.lua' },
  { id: 'self-improvement', labelKey: 'settings.categories.selfImprovement' },
  { id: 'mesnada', labelKey: 'settings.categories.mesnada', group: 'services' },
  { id: 'remembrances', labelKey: 'settings.categories.remembrances', group: 'services' },
  { id: 'snapshots', labelKey: 'settings.categories.snapshots', group: 'services' },
  { id: 'api-server', labelKey: 'settings.categories.apiServer', group: 'services' },
]

export default function SettingsView() {
  const { t } = useTranslation()
  const [activeCategory, setActiveCategory] = useState<SettingsCategory>('general')
  const { connect, disconnect } = useConfigEventsStore()

  // Connect to the config hot-reload SSE stream while this view is mounted.
  useEffect(() => {
    connect()
    return () => { disconnect() }
  }, [connect, disconnect])

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* Mini sidebar */}
      <nav
        style={{
          width: 180,
          flexShrink: 0,
          background: 'var(--sidebar-bg)',
          borderRight: '1px solid var(--border)',
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
              onClick={() => setActiveCategory(cat.id)}
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
              onClick={() => setActiveCategory(cat.id)}
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
      </nav>

      {/* Content area */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '2rem',
          background: 'var(--bg)',
        }}
      >
        {activeCategory === 'general' && <GeneralSettings />}
        {activeCategory === 'providers' && <ProvidersSettings />}
        {activeCategory === 'agents' && <AgentsSettings />}
        {activeCategory === 'mcp-servers' && <MCPServersSettings />}
        {activeCategory === 'mcp-gateway' && <MCPGatewaySettings />}
        {activeCategory === 'lsp' && <LSPSettings />}
        {activeCategory === 'tools' && <InternalToolsSettings />}
        {activeCategory === 'bash' && <BashSettings />}
        {activeCategory === 'skills' && <SkillsSettings />}
        {activeCategory === 'lua' && <LuaSettings />}
        {activeCategory === 'self-improvement' && <EvaluatorSettings />}
        {activeCategory === 'mesnada' && <MesnadaSettings />}
        {activeCategory === 'remembrances' && <RemembrancesSettings />}
        {activeCategory === 'snapshots' && <SnapshotsSettings />}
        {activeCategory === 'api-server' && <APIServerSettings />}
      </div>
    </div>
  )
}
