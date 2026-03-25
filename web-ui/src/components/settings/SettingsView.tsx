import { useEffect, useState } from 'react'
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
  | 'prompts'
  | 'models'
  | 'skills'
  | 'lua'
  | 'evaluator'
  | 'rag'
  | 'mesnada'
  | 'remembrances'
  | 'snapshots'
  | 'api-server'

const CATEGORIES: { id: SettingsCategory; label: string; group?: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'providers', label: 'Providers' },
  { id: 'agents', label: 'Agents' },
  { id: 'mcp-servers', label: 'MCP Servers' },
  { id: 'mcp-gateway', label: 'MCP Gateway' },
  { id: 'lsp', label: 'LSP' },
  { id: 'tools', label: 'Tools' },
  { id: 'bash', label: 'Bash' },
  { id: 'prompts', label: 'Prompts' },
  { id: 'models', label: 'Models' },
  { id: 'skills', label: 'Skills' },
  { id: 'lua', label: 'Lua Engine' },
  { id: 'evaluator', label: 'Evaluator' },
  { id: 'rag', label: 'RAG' },
  { id: 'mesnada', label: 'Mesnada', group: 'services' },
  { id: 'remembrances', label: 'Remembrances', group: 'services' },
  { id: 'snapshots', label: 'Snapshots', group: 'services' },
  { id: 'api-server', label: 'API Server', group: 'services' },
]

function ComingSoon({ name }: { name: string }) {
  return (
    <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
      <p style={{ fontWeight: 600, fontSize: 16, color: 'var(--fg)', marginBottom: '0.5rem' }}>
        {name}
      </p>
      <p>This section is coming soon.</p>
    </div>
  )
}

export default function SettingsView() {
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
        {CATEGORIES.filter((c) => !c.group).map((cat) => {
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
              {cat.label}
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
          Services
        </div>
        {CATEGORIES.filter((c) => c.group === 'services').map((cat) => {
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
              {cat.label}
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
        {activeCategory === 'evaluator' && <EvaluatorSettings />}
        {activeCategory === 'mesnada' && <MesnadaSettings />}
        {activeCategory === 'remembrances' && <RemembrancesSettings />}
        {activeCategory === 'snapshots' && <SnapshotsSettings />}
        {activeCategory === 'api-server' && <APIServerSettings />}
        {activeCategory !== 'general' &&
          activeCategory !== 'providers' &&
          activeCategory !== 'agents' &&
          activeCategory !== 'mcp-servers' &&
          activeCategory !== 'mcp-gateway' &&
          activeCategory !== 'lsp' &&
          activeCategory !== 'tools' &&
          activeCategory !== 'bash' &&
          activeCategory !== 'skills' &&
          activeCategory !== 'lua' &&
          activeCategory !== 'evaluator' &&
          activeCategory !== 'mesnada' &&
          activeCategory !== 'remembrances' &&
          activeCategory !== 'snapshots' &&
          activeCategory !== 'api-server' && (
          <ComingSoon
            name={CATEGORIES.find((c) => c.id === activeCategory)?.label ?? ''}
          />
        )}
      </div>
    </div>
  )
}
