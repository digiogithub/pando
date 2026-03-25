import { useState } from 'react'

export interface KVPair {
  key: string
  value: string
}

interface KeyValueEditorProps {
  label?: string
  pairs: KVPair[]
  onChange: (pairs: KVPair[]) => void
  keyPlaceholder?: string
  valuePlaceholder?: string
}

/** Converts env string array ["KEY=VALUE", ...] to KV pairs */
export function envToKV(env: string[]): KVPair[] {
  return (env ?? []).map((s) => {
    const idx = s.indexOf('=')
    if (idx === -1) return { key: s, value: '' }
    return { key: s.slice(0, idx), value: s.slice(idx + 1) }
  })
}

/** Converts KV pairs to env string array ["KEY=VALUE", ...] */
export function kvToEnv(pairs: KVPair[]): string[] {
  return pairs.filter((p) => p.key.trim()).map((p) => `${p.key}=${p.value}`)
}

export default function KeyValueEditor({
  label,
  pairs,
  onChange,
  keyPlaceholder = 'KEY',
  valuePlaceholder = 'value',
}: KeyValueEditorProps) {
  const [newKey, setNewKey] = useState('')
  const [newValue, setNewValue] = useState('')

  function addPair() {
    if (!newKey.trim()) return
    onChange([...pairs, { key: newKey.trim(), value: newValue }])
    setNewKey('')
    setNewValue('')
  }

  function removePair(idx: number) {
    onChange(pairs.filter((_, i) => i !== idx))
  }

  function updatePair(idx: number, field: 'key' | 'value', val: string) {
    const next = pairs.map((p, i) => (i === idx ? { ...p, [field]: val } : p))
    onChange(next)
  }

  const inputStyle: React.CSSProperties = {
    background: 'var(--input-bg)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm)',
    color: 'var(--fg)',
    fontSize: 13,
    padding: '0.375rem 0.625rem',
    outline: 'none',
    fontFamily: 'monospace',
    width: '100%',
    boxSizing: 'border-box',
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
      {label && (
        <label
          style={{
            fontSize: 12,
            fontWeight: 600,
            color: 'var(--fg-muted)',
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
          }}
        >
          {label}
        </label>
      )}

      {/* Existing pairs */}
      {pairs.map((pair, idx) => (
        <div key={idx} style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <input
            value={pair.key}
            onChange={(e) => updatePair(idx, 'key', e.target.value)}
            placeholder={keyPlaceholder}
            style={{ ...inputStyle, flex: 1 }}
            onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
            onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
          />
          <span style={{ color: 'var(--fg-muted)', fontSize: 13 }}>=</span>
          <input
            value={pair.value}
            onChange={(e) => updatePair(idx, 'value', e.target.value)}
            placeholder={valuePlaceholder}
            style={{ ...inputStyle, flex: 2 }}
            onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
            onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
          />
          <button
            onClick={() => removePair(idx)}
            style={{
              background: 'none',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              cursor: 'pointer',
              color: 'var(--fg-muted)',
              padding: '0.25rem 0.5rem',
              fontSize: 14,
              lineHeight: 1,
              fontFamily: 'inherit',
              flexShrink: 0,
            }}
            title="Remove"
          >
            ×
          </button>
        </div>
      ))}

      {/* New pair row */}
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
        <input
          value={newKey}
          onChange={(e) => setNewKey(e.target.value)}
          placeholder={keyPlaceholder}
          style={{ ...inputStyle, flex: 1 }}
          onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
          onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
          onKeyDown={(e) => { if (e.key === 'Enter') addPair() }}
        />
        <span style={{ color: 'var(--fg-muted)', fontSize: 13 }}>=</span>
        <input
          value={newValue}
          onChange={(e) => setNewValue(e.target.value)}
          placeholder={valuePlaceholder}
          style={{ ...inputStyle, flex: 2 }}
          onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
          onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
          onKeyDown={(e) => { if (e.key === 'Enter') addPair() }}
        />
        <button
          onClick={addPair}
          style={{
            padding: '0.375rem 0.75rem',
            background: 'var(--primary)',
            color: 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
            cursor: 'pointer',
            fontFamily: 'inherit',
            fontWeight: 600,
            flexShrink: 0,
          }}
        >
          Add
        </button>
      </div>
    </div>
  )
}
