import { useEffect, useRef, useState, useCallback } from 'react'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import api from '@pando/client/services/api'
import { useToastStore } from '@pando/client/stores/toastStore'
import { modelMetaLine } from '@/components/shared/ModelCombobox'
import { Badge, IconButton, Kbd } from '@/components/ui'
import { Circle, CircleCheck, Search, X } from '@/components/ui/icons'
import type { BadgeTone } from '@/components/ui'
import '@/styles/overlays.css'

interface ModelInfo {
  id: string
  name: string
  provider: string
  description: string
  badges: string[]
  canReason: boolean
  supportsReasoningEffort: boolean
  /** Pricing and limits, from the provider or the models.dev catalog. Absent (0) when unknown. */
  contextWindow?: number
  costPer1MIn?: number
  costPer1MOut?: number
  knowledge?: string
}

interface ModelsResponse {
  models: ModelInfo[]
}

const BADGE_TONE: Record<string, BadgeTone> = {
  fast: 'success',
  cost: 'warning',
  capable: 'info',
  vision: 'accent',
  reasoning: 'accent',
  thinking: 'accent',
}

const FALLBACK_MODELS: ModelInfo[] = [
  { id: 'claude-opus-4-6', name: 'Claude Opus 4.6', provider: 'anthropic', description: 'Most capable Anthropic model', badges: ['capable', 'fast'], canReason: true, supportsReasoningEffort: false },
  { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', provider: 'anthropic', description: 'Balanced model', badges: ['fast', 'cost'], canReason: true, supportsReasoningEffort: false },
  { id: 'claude-haiku-4-5', name: 'Claude Haiku 4.5', provider: 'anthropic', description: 'Fastest Anthropic model', badges: ['fast', 'cost'], canReason: false, supportsReasoningEffort: false },
  { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai', description: 'OpenAI flagship model', badges: ['fast', 'capable'], canReason: false, supportsReasoningEffort: false },
  { id: 'gpt-4o-mini', name: 'GPT-4o Mini', provider: 'openai', description: 'Smaller, cost-efficient GPT-4o', badges: ['fast', 'cost'], canReason: false, supportsReasoningEffort: false },
  { id: 'gemini-2.0-flash', name: 'Gemini 2.0 Flash', provider: 'google', description: 'Google fast model', badges: ['fast', 'cost'], canReason: false, supportsReasoningEffort: false },
]

/** Extracts the server-provided reason from an api error body (`{"error": "..."}`). */
function serverErrorMessage(err: unknown): string {
  const raw = err instanceof Error ? err.message : String(err)
  try {
    const parsed = JSON.parse(raw) as { error?: string }
    if (parsed && typeof parsed.error === 'string' && parsed.error) return parsed.error
  } catch {
    // Not JSON — fall through to the raw text.
  }
  return raw || 'unknown error'
}

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
      } catch (err) {
        // Show the server's reason instead of a generic failure: the common case
        // is "cannot change model while processing requests" (a run still blocked
        // on a tool), which is actionable only if the user can read it.
        addToast(`Failed to switch model: ${serverErrorMessage(err)}`, 'error')
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
    <div className="ovl-scrim" onClick={close}>
      <div className="ovl-panel ovl-panel--narrow" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="ovl-header">
          <span className="ovl-header-title">Switch Model</span>
          <IconButton aria-label="Close" icon={<X size={14} />} onClick={close} />
        </div>

        {/* Search */}
        <div className="ovl-search">
          <Search size={13} />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search models..."
            className="ovl-search-input"
          />
        </div>

        {/* Models list */}
        <div ref={listRef} className="ovl-list">
          {loading ? (
            <div className="ovl-empty">Loading models...</div>
          ) : flatModels.length === 0 ? (
            <div className="ovl-empty">No models found</div>
          ) : (
            providers.map((provider) => {
              const providerModels = filtered.filter((m) => m.provider === provider)
              if (providerModels.length === 0) return null
              const providerOffset = flatModels.findIndex((m) => m.provider === provider)
              return (
                <div key={provider}>
                  <div className="ovl-group-label">{provider}</div>
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
                        className="ovl-item ovl-model-row"
                      >
                        <span className={isActive ? 'ovl-model-radio ovl-model-radio--active' : 'ovl-model-radio'}>
                          {isActive ? <CircleCheck size={15} /> : <Circle size={10} />}
                        </span>
                        <div className="ovl-model-info">
                          <div className={isActive ? 'ovl-model-name ovl-model-name--active' : 'ovl-model-name'}>{model.name}</div>
                          {model.description && <div className="ovl-model-desc">{model.description}</div>}
                          {modelMetaLine(model) && <div className="ovl-model-meta">{modelMetaLine(model)}</div>}
                        </div>
                        <div className="ovl-model-badges">
                          {model.canReason && (
                            <Badge tone="accent" outline title="Supports extended thinking / reasoning">thinking</Badge>
                          )}
                          {model.badges.map((badge) => (
                            <Badge key={badge} tone={BADGE_TONE[badge] ?? 'neutral'}>{badge}</Badge>
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
        <div className="ovl-footer">
          <span><Kbd>↑↓</Kbd> navigate</span>
          <span><Kbd>Enter</Kbd> select</span>
          <span><Kbd>Esc</Kbd> close</span>
        </div>
      </div>
    </div>
  )
}
