import { useCallback, useEffect, useRef, useState } from 'react'
import api from '@/services/api'

interface ModelInfo {
  id: string
  name: string
  provider: string
  badges: string[]
}

const BADGE_COLORS: Record<string, string> = {
  fast: 'var(--success)',
  cost: '#F59E0B',
  capable: 'var(--info)',
}

const inputStyle: React.CSSProperties = {
  background: 'var(--input-bg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 14,
  padding: '0.5rem 0.75rem',
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
  boxSizing: 'border-box',
  cursor: 'pointer',
  textAlign: 'left',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  transition: 'border-color 0.15s',
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
  const [selectedIndex, setSelectedIndex] = useState(0)
  const containerRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const fetchedRef = useRef(false)
  const openDropdown = useCallback(() => {
    setOpen(true)
    setQuery('')
    setSelectedIndex(0)
    if (!fetchedRef.current) {
      fetchedRef.current = true
      api
        .get<{ models: ModelInfo[] }>('/api/v1/models')
        .then((r) => setModels(r.models))
        .catch(() => {})
    }
    setTimeout(() => searchRef.current?.focus(), 0)
  }, [])

  const closeDropdown = useCallback(() => {
    setOpen(false)
    setQuery('')
  }, [])

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        closeDropdown()
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open, closeDropdown])

  const q = query.toLowerCase()
  const filtered = models.filter(
    (m) =>
      !q ||
      m.name.toLowerCase().includes(q) ||
      m.id.toLowerCase().includes(q) ||
      m.provider.toLowerCase().includes(q),
  )

  const providers = [...new Set(filtered.map((m) => m.provider))]
  const flatModels = providers.flatMap((p) => filtered.filter((m) => m.provider === p))

  useEffect(() => {
    setSelectedIndex(0)
  }, [query])

  useEffect(() => {
    if (!open) return
    const el = listRef.current?.querySelector<HTMLElement>('[data-selected="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [selectedIndex, open])

  const selectModel = useCallback(
    (m: ModelInfo) => {
      onChange(m.id)
      onSelect?.(m)
      closeDropdown()
    },
    [onChange, onSelect, closeDropdown],
  )

  return (
    <div ref={containerRef} style={{ position: 'relative', width: '100%' }}>
      <button
        type="button"
        onClick={open ? closeDropdown : openDropdown}
        style={inputStyle as React.CSSProperties}
        onFocus={(e) => { e.currentTarget.style.borderColor = 'var(--border-focus)' }}
        onBlur={(e) => { if (!open) e.currentTarget.style.borderColor = 'var(--border)' }}
      >
        <span style={{ flex: 1, textAlign: 'left', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {value || <span style={{ color: 'var(--fg-dim)' }}>{placeholder}</span>}
        </span>
        <span style={{ color: 'var(--fg-dim)', fontSize: 11, marginLeft: '0.5rem', flexShrink: 0 }}>▼</span>
      </button>

      {open && (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            left: 0,
            right: 0,
            zIndex: 200,
            background: 'var(--card-bg, var(--input-bg))',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius)',
            boxShadow: '0 8px 24px rgba(0,0,0,0.16)',
            display: 'flex',
            flexDirection: 'column',
            maxHeight: 320,
            overflow: 'hidden',
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') { closeDropdown(); return }
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setSelectedIndex((i) => Math.min(i + 1, flatModels.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setSelectedIndex((i) => Math.max(i - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              const m = flatModels[selectedIndex]
              if (m) selectModel(m)
            }
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              padding: '0.5rem 0.75rem',
              borderBottom: '1px solid var(--border)',
              gap: '0.5rem',
            }}
          >
            <span style={{ color: 'var(--fg-dim)', fontSize: 12 }}>⌕</span>
            <input
              ref={searchRef}
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
                fontFamily: 'inherit',
              }}
            />
          </div>

          <div ref={listRef} style={{ overflowY: 'auto', flex: 1 }}>
            {models.length === 0 ? (
              <div style={{ padding: '1rem', textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>
                Loading models…
              </div>
            ) : flatModels.length === 0 ? (
              <div style={{ padding: '1rem', textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>
                No models found
              </div>
            ) : (
              providers.map((provider) => {
                const pModels = filtered.filter((m) => m.provider === provider)
                if (pModels.length === 0) return null
                const pOffset = flatModels.findIndex((m) => m.provider === provider)
                return (
                  <div key={provider}>
                    <div
                      style={{
                        padding: '0.375rem 0.75rem 0.125rem',
                        fontSize: 10,
                        fontWeight: 700,
                        color: 'var(--fg-dim)',
                        textTransform: 'uppercase',
                        letterSpacing: '0.06em',
                      }}
                    >
                      {provider}
                    </div>
                    {pModels.map((model, idx) => {
                      const flatIdx = pOffset + idx
                      const isSelected = selectedIndex === flatIdx
                      const isActive = model.id === value
                      return (
                        <div
                          key={model.id}
                          data-selected={isSelected ? 'true' : undefined}
                          onClick={() => selectModel(model)}
                          onMouseEnter={() => setSelectedIndex(flatIdx)}
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: '0.5rem',
                            padding: '0.375rem 0.75rem',
                            cursor: 'pointer',
                            background: isSelected ? 'var(--selected, rgba(99,102,241,0.08))' : 'transparent',
                            fontSize: 13,
                            color: isActive ? 'var(--primary)' : 'var(--fg)',
                            fontWeight: isActive ? 600 : 400,
                          }}
                        >
                          <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {model.id}
                          </span>
                          <div style={{ display: 'flex', gap: 3, flexShrink: 0 }}>
                            {model.badges.map((badge) => (
                              <span
                                key={badge}
                                style={{
                                  fontSize: 9,
                                  fontWeight: 600,
                                  padding: '1px 5px',
                                  borderRadius: 3,
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
        </div>
      )}
    </div>
  )
}
