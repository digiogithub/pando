import { useCallback, useEffect, useRef, useState } from 'react'
import { Popover } from '@/components/ui'
import { ChevronDown, Search, TriangleAlert } from '@/components/ui/icons'
import api from '@pando/client/services/api'

interface ModelInfo {
  id: string
  name: string
  provider: string
  badges: string[]
  /** Pricing and limits, from the provider or the models.dev catalog. Absent (0) when unknown. */
  contextWindow?: number
  costPer1MIn?: number
  costPer1MOut?: number
  knowledge?: string
}

/** 200000 -> "200K", 1000000 -> "1M". */
export function formatTokenLimit(tokens: number): string {
  if (tokens >= 1_000_000) {
    const value = tokens / 1_000_000
    return `${Number.isInteger(value) ? value : value.toFixed(1)}M`
  }
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}K`
  return `${tokens}`
}

/**
 * Metadata line for a model: context window, per-million-token prices and
 * training cutoff. Each piece is omitted when unknown — a missing price must
 * never read as "free".
 */
export function modelMetaLine(model: {
  contextWindow?: number
  costPer1MIn?: number
  costPer1MOut?: number
  knowledge?: string
}): string {
  const parts: string[] = []
  if (model.contextWindow) parts.push(`${formatTokenLimit(model.contextWindow)} ctx`)
  if (model.costPer1MIn || model.costPer1MOut) {
    parts.push(`$${model.costPer1MIn ?? 0}/$${model.costPer1MOut ?? 0} per 1M`)
  }
  if (model.knowledge) parts.push(`cutoff ${model.knowledge}`)
  return parts.join(' · ')
}

export default function ModelCombobox({
  value,
  onChange,
  onSelect,
  placeholder = 'e.g. claude-sonnet-4-6',
}: {
  value: string
  onChange: (v: string) => void
  onSelect?: (m: ModelInfo) => void
  placeholder?: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [models, setModels] = useState<ModelInfo[]>([])
  const [providerErrors, setProviderErrors] = useState<Record<string, string>>({})
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [triggerWidth, setTriggerWidth] = useState<number>()
  const buttonRef = useRef<HTMLButtonElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const [fetching, setFetching] = useState(false)
  const fetchedRef = useRef(false)

  const openDropdown = useCallback(() => {
    setTriggerWidth(buttonRef.current?.offsetWidth)
    setOpen(true)
    setQuery('')
    setSelectedIndex(0)
    if (!fetchedRef.current) {
      fetchedRef.current = true
      setFetching(true)
      api
        .get<{ models: ModelInfo[]; errors?: Record<string, string> }>('/api/v1/models')
        .then((r) => {
          setModels(r.models)
          setProviderErrors(r.errors ?? {})
        })
        .catch(() => {})
        .finally(() => setFetching(false))
    }
    setTimeout(() => searchRef.current?.focus(), 0)
  }, [])

  const closeDropdown = useCallback(() => {
    setOpen(false)
    setQuery('')
  }, [])

  const q = query.toLowerCase()
  // Support "provider.model" syntax: if query contains a dot, split into provider prefix and model filter
  const dotIdx = q.indexOf('.')
  const providerPrefix = dotIdx > 0 ? q.slice(0, dotIdx) : null
  const modelSuffix = dotIdx > 0 ? q.slice(dotIdx + 1) : q

  const filtered = models.filter((m) => {
    if (!q) return true
    const provider = m.provider.toLowerCase()
    const id = m.id.toLowerCase()
    const name = m.name.toLowerCase()
    if (providerPrefix !== null) {
      // Must match provider prefix AND model suffix
      return provider.includes(providerPrefix) && (modelSuffix === '' || id.includes(modelSuffix) || name.includes(modelSuffix))
    }
    return id.includes(q) || name.includes(q) || provider.includes(q)
  })

  const providers = [...new Set(filtered.map((m) => m.provider))]
  const flatModels = providers.flatMap((p) => filtered.filter((m) => m.provider === p))
  const normalizedSelectedIndex = query ? 0 : selectedIndex

  useEffect(() => {
    if (!open) return
    const el = listRef.current?.querySelector<HTMLElement>('[data-active="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [normalizedSelectedIndex, open])

  const selectModel = useCallback(
    (m: ModelInfo) => {
      onChange(m.id)
      onSelect?.(m)
      closeDropdown()
    },
    [onChange, onSelect, closeDropdown],
  )

  const activeModel = models.find((m) => m.id === value)

  return (
    <div className="relative w-full">
      <button
        ref={buttonRef}
        type="button"
        onClick={open ? closeDropdown : openDropdown}
        className="model-combo-trigger"
      >
        <span className="model-combo-value">
          {value ? (
            <>
              {activeModel && <span className="model-combo-provider-tag">{activeModel.provider}</span>}
              <span>{value}</span>
            </>
          ) : (
            <span className="model-combo-placeholder">{placeholder}</span>
          )}
        </span>
        <ChevronDown size={14} className="model-combo-chevron" />
      </button>

      <Popover open={open} onClose={closeDropdown} anchorRef={buttonRef} placement="bottom-start" padded={false} className="model-combo-popover">
        <div
          className="model-combo-panel"
          // Minus the popover's 1px border on each side so it lines up with the trigger.
          style={{ width: triggerWidth ? triggerWidth - 2 : undefined }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault()
              closeDropdown()
              return
            }
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setSelectedIndex((i) => Math.min(i + 1, flatModels.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setSelectedIndex((i) => Math.max(i - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              const m = flatModels[normalizedSelectedIndex]
              if (m) selectModel(m)
            }
          }}
        >
          <div className="model-combo-search">
            <Search size={13} />
            <input
              ref={searchRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search models..."
            />
          </div>

          {/* Per-provider error warnings */}
          {Object.keys(providerErrors).length > 0 && (
            <div className="border-b border-border">
              {Object.entries(providerErrors).map(([prov, msg]) => (
                <div key={prov} className="model-combo-error">
                  <TriangleAlert size={11} className="mt-0.5 shrink-0" />
                  <span>
                    <strong className="font-semibold">{prov}:</strong> {msg}
                  </span>
                </div>
              ))}
            </div>
          )}

          <div ref={listRef} className="model-combo-list">
            {fetching ? (
              <div className="p-4 text-center text-sm text-muted">Loading models…</div>
            ) : models.length === 0 ? (
              <div className="p-4 text-center text-sm text-muted">No models found — configure a provider first</div>
            ) : flatModels.length === 0 ? (
              <div className="p-4 text-center text-sm text-muted">No models found</div>
            ) : (
              providers.map((provider) => {
                const pModels = filtered.filter((m) => m.provider === provider)
                if (pModels.length === 0) return null
                const pOffset = flatModels.findIndex((m) => m.provider === provider)
                return (
                  <div key={provider}>
                    <div className="model-combo-group-label">{provider}</div>
                    {pModels.map((model, idx) => {
                      const flatIdx = pOffset + idx
                      const isSelected = normalizedSelectedIndex === flatIdx
                      const isActive = model.id === value
                      return (
                        <div
                          key={model.id}
                          data-active={isSelected ? 'true' : undefined}
                          data-current={isActive ? 'true' : undefined}
                          onClick={() => selectModel(model)}
                          onMouseEnter={() => setSelectedIndex(flatIdx)}
                          className="model-combo-option"
                        >
                          <span className="min-w-0 flex-1">
                            <span className="model-combo-option-id">{model.id}</span>
                            {modelMetaLine(model) && (
                              <span className="model-combo-option-meta">{modelMetaLine(model)}</span>
                            )}
                          </span>
                          <div className="model-combo-badges">
                            {model.badges.map((badge) => (
                              <span key={badge} className={`model-combo-badge model-combo-badge--${badge}`}>
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
        </div>
      </Popover>
    </div>
  )
}
