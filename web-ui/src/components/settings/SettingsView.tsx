import { useState } from 'react'
import GeneralSettings from './GeneralSettings'

type SettingsCategory =
  | 'general'
  | 'providers'
  | 'tools'
  | 'prompts'
  | 'models'
  | 'plugins'
  | 'rag'

const CATEGORIES: { id: SettingsCategory; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'providers', label: 'Providers' },
  { id: 'tools', label: 'Tools' },
  { id: 'prompts', label: 'Prompts' },
  { id: 'models', label: 'Models' },
  { id: 'plugins', label: 'Plugins' },
  { id: 'rag', label: 'RAG' },
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
        {CATEGORIES.map((cat) => {
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
        {activeCategory !== 'general' && (
          <ComingSoon
            name={CATEGORIES.find((c) => c.id === activeCategory)?.label ?? ''}
          />
        )}
      </div>
    </div>
  )
}
