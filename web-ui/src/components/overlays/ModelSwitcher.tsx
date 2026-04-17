import { useEffect, useRef, useState, useCallback } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faMagnifyingGlass,
  faTimes,
  faCircle,
  faCircleDot,
} from '@fortawesome/free-solid-svg-icons'
import { useLayoutStore } from '@/stores/layoutStore'
import { useSettingsStore } from '@/stores/settingsStore'
import api from '@/services/api'
import { useToastStore } from '@/stores/toastStore'

interface ModelInfo {
  id: string
  name: string
  provider: string
  description: string
  badges: string[]
}

interface ModelsResponse {
  models: ModelInfo[]
}

const BADGE_COLORS: Record<string, string> = {
  fast: 'var(--success)',
  cost: '#F59E0B',
  capable: 'var(--info)',
  vision: 'var(--accent)',
}

const FALLBACK_MODELS: ModelInfo[] = [
  { id: 'claude-opus-4-6', name: 'Claude Opus 4.6', provider: 'anthropic', description: 'Most capable Anthropic model', badges: ['capable', 'fast'] },
  { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', provider: 'anthropic', description: 'Balanced model', badges: ['fast', 'cost'] },
  { id: 'claude-haiku-4-5', name: 'Claude Haiku 4.5', provider: 'anthropic', description: 'Fastest Anthropic model', badges: ['fast', 'cost'] },
  { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai', description: 'OpenAI flagship model', badges: ['fast', 'capable'] },
  { id: 'gpt-4o-mini', name: 'GPT-4o Mini', provider: 'openai', description: 'Smaller, cost-efficient GPT-4o', badges: ['fast', 'cost'] },
  { id: 'gemini-2.0-flash', name: 'Gemini 2.0 Flash', provider: 'google', description: 'Google fast model', badges: ['fast', 'cost'] },
]

export default function ModelSwitcher() {
  const { setModelSwitcherOpen } = useLayoutStore()
  const { config, updateField } = useSettingsStore()
  const addToast = useToastStore((s) => s.addToast)
  const [query, setQuery] = useState('')
  const [models, setModels] = useState<ModelInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const hasLoadedRef = useRef(false)

  const close = useCallback(() => setModelSwitcherOpen(false), [setModelSwitcherOpen])

  useEffect(() => {
    inputRef.current?.focus()
    if (hasLoadedRef.current) return
    hasLoadedRef.current = true
    api
      .get<ModelsResponse>('/api/v1/models')
      .then((resp) => setModels(resp.models))
      .catch(() => setModels(FALLBACK_MODELS))
      .finally(() => setLoading(false))
  }, [])

  const q = query.toLowerCase()
  const filtered = models.filter(
    (m) =>
      !q ||
      m.name.toLowerCase().includes(q) ||
      m.id.toLowerCase().includes(q) ||
      m.provider.toLowerCase().includes(q),
  )

  // Group by provider
  const providers = [...new Set(filtered.map((m) => m.provider))]

  const flatModels = providers.flatMap((p) => filtered.filter((m) => m.provider === p))

  const normalizedSelectedIndex = query ? 0 : selectedIndex

  const selectModel = useCallback(
    async (modelId: string) => {
      try {
        await api.put<{ model: string }>('/api/v1/models/active', { model: modelId })
        updateField('default_model', modelId)
        addToast(`Model switched to ${modelId}`, 'success')
        close()
      } catch {
        addToast('Failed to switch model', 'error')
      }
    },
    [close, updateField, addToast],
  )

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { close(); return }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex((i) => Math.min(i + 1, flatModels.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex((i) => Math.max(i - 1, 0))
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        const m = flatModels[normalizedSelectedIndex]
        if (m) selectModel(m.id)
        return
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [flatModels, normalizedSelectedIndex, selectModel, close])

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-selected="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [normalizedSelectedIndex])

  const activeModel = config.default_model

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        background: 'rgba(0,0,0,0.5)',
        display: 'flex',
        alignItems: 'flex-start',
        justifyContent: 'center',
        paddingTop: '10vh',
        animation: 'qm-fade-in 0.15s ease',
      }}
      onClick={close}
    >
      <style>{`
        @keyframes qm-fade-in { from { opacity:0; } to { opacity:1; } }
        @keyframes qm-slide-up {
          from { opacity:0; transform:translateY(12px) scale(0.98); }
          to   { opacity:1; transform:translateY(0) scale(1); }
        }
      `}</style>
      <div
        style={{
          width: 560,
          maxWidth: 'calc(100vw - 2rem)',
          maxHeight: 480,
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          boxShadow: '0 16px 48px rgba(0,0,0,0.24)',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          animation: 'qm-slide-up 0.15s ease',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            padding: '0.875rem 1rem',
            borderBottom: '1px solid var(--border)',
            gap: '0.75rem',
          }}
        >
          <span style={{ flex: 1, fontWeight: 600, fontSize: 15, color: 'var(--fg)' }}>
            Switch Model
          </span>
          <button
            onClick={close}
            style={{
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              color: 'var(--fg-dim)',
              fontSize: 14,
              padding: '0.25rem',
              display: 'flex',
              alignItems: 'center',
            }}
          >
            <FontAwesomeIcon icon={faTimes} />
          </button>
        </div>

        {/* Search */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.75rem',
            padding: '0.625rem 1rem',
            borderBottom: '1px solid var(--border)',
          }}
        >
          <FontAwesomeIcon
            icon={faMagnifyingGlass}
            style={{ color: 'var(--fg-dim)', fontSize: 13, flexShrink: 0 }}
          />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search models..."
            style={{
              flex: 1,
              background: 'none',
              border: 'none',
              outline: 'none',
              fontSize: 13,
              color: 'var(--fg)',
            }}
          />
        </div>

        {/* Models list */}
        <div ref={listRef} style={{ flex: 1, overflowY: 'auto', padding: '0.25rem 0' }}>
          {loading ? (
            <div style={{ padding: '2rem', textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>
              Loading models...
            </div>
          ) : flatModels.length === 0 ? (
            <div style={{ padding: '2rem', textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>
              No models found
            </div>
          ) : (
            providers.map((provider) => {
              const providerModels = filtered.filter((m) => m.provider === provider)
              if (providerModels.length === 0) return null
              const providerOffset = flatModels.findIndex((m) => m.provider === provider)
              return (
                <div key={provider}>
                  <div
                    style={{
                      padding: '0.5rem 1rem 0.25rem',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--fg-dim)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.06em',
                    }}
                  >
                    {provider}
                  </div>
                  {providerModels.map((model, idx) => {
                    const flatIdx = providerOffset + idx
                    const isSelected = normalizedSelectedIndex === flatIdx
                    const isActive = model.id === activeModel
                    return (
                      <div
                        key={model.id}
                        data-selected={isSelected ? 'true' : undefined}
                        onClick={() => selectModel(model.id)}
                        onMouseEnter={() => setSelectedIndex(flatIdx)}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: '0.75rem',
                          padding: '0.5rem 1rem',
                          cursor: 'pointer',
                          background: isSelected ? 'var(--selected)' : 'transparent',
                          borderRadius: 'var(--radius-sm)',
                          margin: '1px 0.5rem',
                          transition: 'background 0.1s',
                        }}
                      >
                        <FontAwesomeIcon
                          icon={isActive ? faCircleDot : faCircle}
                          style={{
                            fontSize: isActive ? 14 : 10,
                            color: isActive ? 'var(--primary)' : 'var(--fg-dim)',
                            flexShrink: 0,
                            width: 16,
                          }}
                        />
                        <div style={{ flex: 1 }}>
                          <div
                            style={{
                              fontSize: 13,
                              color: 'var(--fg)',
                              fontWeight: isActive ? 600 : 400,
                            }}
                          >
                            {model.name}
                          </div>
                          {model.description && (
                            <div style={{ fontSize: 11, color: 'var(--fg-muted)' }}>
                              {model.description}
                            </div>
                          )}
                        </div>
                        <div style={{ display: 'flex', gap: '0.25rem' }}>
                          {model.badges.map((badge) => (
                            <span
                              key={badge}
                              style={{
                                fontSize: 10,
                                fontWeight: 600,
                                padding: '1px 6px',
                                borderRadius: 0,
                                background: `${BADGE_COLORS[badge] ?? 'var(--fg-dim)'}22`,
                                color: BADGE_COLORS[badge] ?? 'var(--fg-dim)',
                                border: `1px solid ${BADGE_COLORS[badge] ?? 'var(--fg-dim)'}44`,
                                textTransform: 'lowercase',
                              }}
                            >
                              {badge}
                            </span>
                          ))}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )
            })
          )}
        </div>

        {/* Footer hint */}
        <div
          style={{
            padding: '0.5rem 1rem',
            borderTop: '1px solid var(--border)',
            display: 'flex',
            gap: '1rem',
            fontSize: 11,
            color: 'var(--fg-dim)',
          }}
        >
          <span><kbd style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 0, padding: '0 4px' }}>↑↓</kbd> navigate</span>
          <span><kbd style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 0, padding: '0 4px' }}>Enter</kbd> select</span>
          <span><kbd style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 0, padding: '0 4px' }}>Esc</kbd> close</span>
        </div>
      </div>
    </div>
  )
}
