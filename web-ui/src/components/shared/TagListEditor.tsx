import { useState, KeyboardEvent } from 'react'
import { Button, Input } from '@/components/ui'

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
    <div className="flex flex-col gap-2">
      {label && <label className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</label>}

      {/* Tags */}
      {items.length > 0 && (
        <div className="tag-list">
          {items.map((item, idx) => (
            <span key={idx} className="tag-chip">
              {item}
              <button onClick={() => removeItem(idx)} className="tag-chip-remove" title="Remove">
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Input row */}
      <div className="flex gap-2">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          className="flex-1 font-mono"
        />
        <Button onClick={addItem}>Add</Button>
      </div>
    </div>
  )
}
