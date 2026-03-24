import { useState, useRef, useEffect } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlay } from '@fortawesome/free-solid-svg-icons'
import { useTerminalStore } from '@/stores/terminalStore'

export default function TerminalInput() {
  const { execCommand, running, history, historyIndex, setHistoryIndex } = useTerminalStore()
  const [value, setValue] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  // Focus input on mount
  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  function handleSubmit(e?: React.FormEvent) {
    e?.preventDefault()
    if (!value.trim() || running) return
    execCommand(value.trim())
    setValue('')
    setHistoryIndex(-1)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      handleSubmit()
      return
    }

    if (e.key === 'ArrowUp') {
      e.preventDefault()
      const nextIndex = Math.min(historyIndex + 1, history.length - 1)
      setHistoryIndex(nextIndex)
      if (history[nextIndex] !== undefined) {
        setValue(history[nextIndex])
      }
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      const nextIndex = Math.max(historyIndex - 1, -1)
      setHistoryIndex(nextIndex)
      setValue(nextIndex === -1 ? '' : history[nextIndex] ?? '')
      return
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
        disabled={running}
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
        disabled={running || !value.trim()}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.3rem',
          padding: '0.35rem 0.75rem',
          background: running || !value.trim() ? '#21262d' : 'var(--primary)',
          border: 'none',
          borderRadius: 'var(--radius-sm)',
          cursor: running || !value.trim() ? 'not-allowed' : 'pointer',
          color: running || !value.trim() ? '#6e7681' : 'white',
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
