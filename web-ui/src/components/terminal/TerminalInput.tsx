import { useEffect, useRef, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlay } from '@fortawesome/free-solid-svg-icons'
import type { TerminalTab } from '@/stores/terminalStore'
import { useTerminalStore } from '@/stores/terminalStore'

interface TerminalInputProps {
  tab: TerminalTab
  focusKey?: number
}

export default function TerminalInput({ tab, focusKey = 0 }: TerminalInputProps) {
  const { execCommand, setHistoryIndex } = useTerminalStore()
  const [value, setValue] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [tab.id, focusKey])

  function handleSubmit(e?: React.FormEvent) {
    e?.preventDefault()
    if (!value.trim() || tab.running) return
    void execCommand(value.trim(), tab.id)
    setValue('')
    setHistoryIndex(-1, tab.id)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      handleSubmit()
      return
    }

    if (e.key === 'ArrowUp') {
      e.preventDefault()
      const nextIndex = Math.min(tab.historyIndex + 1, tab.history.length - 1)
      setHistoryIndex(nextIndex, tab.id)
      if (tab.history[nextIndex] !== undefined) {
        setValue(tab.history[nextIndex])
      }
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      const nextIndex = Math.max(tab.historyIndex - 1, -1)
      setHistoryIndex(nextIndex, tab.id)
      setValue(nextIndex === -1 ? '' : tab.history[nextIndex] ?? '')
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.5rem',
        padding: '0.5rem 0.75rem',
        background: '#161b22',
        borderTop: '1px solid #30363d',
        flexShrink: 0,
      }}
    >
      <span
        style={{
          color: '#3fb950',
          fontFamily: 'monospace',
          fontSize: 14,
          fontWeight: 700,
          flexShrink: 0,
        }}
      >
        $
      </span>
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="type a command..."
        disabled={tab.running}
        style={{
          flex: 1,
          background: 'transparent',
          border: 'none',
          outline: 'none',
          color: '#e2e8f0',
          fontFamily: 'monospace',
          fontSize: 13,
          caretColor: '#3fb950',
        }}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
      />
      <button
        type="submit"
        disabled={tab.running || !value.trim()}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.3rem',
          padding: '0.35rem 0.75rem',
          background: tab.running || !value.trim() ? '#21262d' : 'var(--primary)',
          border: 'none',
          borderRadius: 'var(--radius-sm)',
          cursor: tab.running || !value.trim() ? 'not-allowed' : 'pointer',
          color: tab.running || !value.trim() ? '#6e7681' : 'white',
          fontSize: 12,
          fontWeight: 600,
          flexShrink: 0,
          transition: 'background 0.15s ease',
        }}
      >
        <FontAwesomeIcon icon={faPlay} style={{ fontSize: 10 }} />
        Run
      </button>
    </form>
  )
}
