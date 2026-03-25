import { useState, KeyboardEvent } from 'react'

interface TagListEditorProps {
  label?: string
  items: string[]
  onChange: (items: string[]) => void
  placeholder?: string
}

export default function TagListEditor({ label, items, onChange, placeholder = 'Add item…' }: TagListEditorProps) {
  const [input, setInput] = useState('')

  function addItem() {
    const val = input.trim()
    if (val && !items.includes(val)) {
      onChange([...items, val])
    }
    setInput('')
  }

  function removeItem(idx: number) {
    onChange(items.filter((_, i) => i !== idx))
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      addItem()
    }
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

      {/* Tags */}
      {items.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.375rem' }}>
          {items.map((item, idx) => (
            <span
              key={idx}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
                background: 'var(--selected)',
                color: 'var(--fg)',
                borderRadius: 'var(--radius-sm)',
                padding: '0.125rem 0.5rem',
                fontSize: 12,
                fontFamily: 'monospace',
              }}
            >
              {item}
              <button
                onClick={() => removeItem(idx)}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  color: 'var(--fg-muted)',
                  padding: '0 0.125rem',
                  lineHeight: 1,
                  fontSize: 14,
                  fontFamily: 'inherit',
                }}
                title="Remove"
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Input row */}
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          style={{
            flex: 1,
            background: 'var(--input-bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--fg)',
            fontSize: 13,
            padding: '0.375rem 0.625rem',
            outline: 'none',
            fontFamily: 'monospace',
          }}
          onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
          onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
        />
        <button
          onClick={addItem}
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
          }}
        >
          Add
        </button>
      </div>
    </div>
  )
}
