import { useRef, useState } from 'react'
import { Button, Input } from '@/components/ui'
import { X } from '@/components/ui/icons'

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
  const draftRowRef = useRef<HTMLDivElement>(null)

  function addPair() {
    if (!newKey.trim()) return
    onChange([...pairs, { key: newKey.trim(), value: newValue }])
    setNewKey('')
    setNewValue('')
  }

  /**
   * Commits the half-typed draft row when focus leaves it entirely (e.g. the
   * user types KEY/value and clicks "Save" straight away without pressing
   * "Add" or Enter). Without this the draft lives only in local state and is
   * silently dropped when the parent form is submitted. Moving between the two
   * inputs of the draft row keeps focus inside it and must NOT commit.
   */
  function commitDraftOnLeave(e: React.FocusEvent<HTMLDivElement>) {
    const next = e.relatedTarget as Node | null
    if (next && draftRowRef.current?.contains(next)) return
    addPair()
  }

  function removePair(idx: number) {
    onChange(pairs.filter((_, i) => i !== idx))
  }

  function updatePair(idx: number, field: 'key' | 'value', val: string) {
    const next = pairs.map((p, i) => (i === idx ? { ...p, [field]: val } : p))
    onChange(next)
  }

  return (
    <div className="flex flex-col gap-2">
      {label && <label className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</label>}

      {/* Existing pairs */}
      {pairs.map((pair, idx) => (
        <div key={idx} className="kv-row">
          <Input
            value={pair.key}
            onChange={(e) => updatePair(idx, 'key', e.target.value)}
            placeholder={keyPlaceholder}
            className="kv-input"
          />
          <span className="text-sm text-muted">=</span>
          <Input
            value={pair.value}
            onChange={(e) => updatePair(idx, 'value', e.target.value)}
            placeholder={valuePlaceholder}
            className="kv-input kv-input--value"
          />
          <button onClick={() => removePair(idx)} className="kv-remove" title="Remove">
            <X size={14} />
          </button>
        </div>
      ))}

      {/* New pair row */}
      <div ref={draftRowRef} onBlur={commitDraftOnLeave} className="kv-row">
        <Input
          value={newKey}
          onChange={(e) => setNewKey(e.target.value)}
          placeholder={keyPlaceholder}
          className="kv-input"
          onKeyDown={(e) => {
            if (e.key === 'Enter') addPair()
          }}
        />
        <span className="text-sm text-muted">=</span>
        <Input
          value={newValue}
          onChange={(e) => setNewValue(e.target.value)}
          placeholder={valuePlaceholder}
          className="kv-input kv-input--value"
          onKeyDown={(e) => {
            if (e.key === 'Enter') addPair()
          }}
        />
        <Button onClick={addPair} className="shrink-0">
          Add
        </Button>
      </div>
    </div>
  )
}
