import { useEffect, useRef, useState } from 'react'
import type { TerminalTab } from '@pando/client/stores/terminalStore'
import { useTerminalStore } from '@pando/client/stores/terminalStore'
import { Play } from '@/components/ui/icons'

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
    <form onSubmit={handleSubmit} className="terminal-input-form">
      <span className="terminal-input-prompt">$</span>
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="type a command..."
        disabled={tab.running}
        className="terminal-input-field"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
      />
      <button type="submit" disabled={tab.running || !value.trim()} className="terminal-run-btn">
        <Play size={11} />
        Run
      </button>
    </form>
  )
}
