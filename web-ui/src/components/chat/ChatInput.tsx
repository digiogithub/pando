import { useRef, useState, useEffect, useCallback, type KeyboardEvent, type ChangeEvent } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPaperPlane, faStop } from '@fortawesome/free-solid-svg-icons'
import SlashCommandMenu, { type SlashCommandItem } from './SlashCommandMenu'
import api from '@/services/api'

const MAX_CHARS = 8000
// 6 lines × (14px font × 1.5 line-height) = 126px
const LINE_HEIGHT = 21 // 14px × 1.5
const MAX_LINES = 6
const MAX_TEXTAREA_HEIGHT = LINE_HEIGHT * MAX_LINES // 126px

interface ChatInputProps {
  onSend: (text: string) => void
  streaming: boolean
  onCancel: () => void
  disabled?: boolean
  goalActive?: boolean
}

export default function ChatInput({ onSend, streaming, onCancel, disabled, goalActive = false }: ChatInputProps) {
  const [value, setValue] = useState('')
  const [focused, setFocused] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Slash command menu state
  const [slashMenuOpen, setSlashMenuOpen] = useState(false)
  const [slashFilter, setSlashFilter] = useState('')
  const [slashSelectedIdx, setSlashSelectedIdx] = useState(0)
  const [slashCommands, setSlashCommands] = useState<SlashCommandItem[]>([])

  // Fetch available commands on mount
  useEffect(() => {
    api.get<SlashCommandItem[]>('/api/v1/commands')
      .then((cmds) => setSlashCommands(Array.isArray(cmds) ? cmds : []))
      .catch(() => {})
  }, [])

  // Auto-resize: grow up to MAX_LINES, then scroll
  const resize = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const newHeight = Math.min(el.scrollHeight, MAX_TEXTAREA_HEIGHT)
    el.style.height = `${newHeight}px`
    // When at max height, keep scroll at bottom so latest line is visible
    if (el.scrollHeight > MAX_TEXTAREA_HEIGHT) {
      el.scrollTop = el.scrollHeight
    }
  }, [])

  useEffect(() => {
    resize()
  }, [value, resize])

  // Compute filtered commands for the menu
  const filteredCommands = slashCommands.filter((cmd) =>
    cmd.name.toLowerCase().startsWith(slashFilter.toLowerCase())
  )

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    setValue(newValue)

    // Detect slash command: value starts with "/" and is on a single line
    if (newValue.startsWith('/') && !newValue.includes('\n')) {
      const filter = newValue.slice(1) // strip leading "/"
      setSlashFilter(filter)
      setSlashSelectedIdx(0)
      setSlashMenuOpen(true)
    } else {
      setSlashMenuOpen(false)
    }
  }

  const handleSlashSelect = (cmd: SlashCommandItem) => {
    const newValue = '/' + cmd.name + (cmd.acceptsArgs ? ' ' : '')
    setValue(newValue)
    setSlashMenuOpen(false)
    textareaRef.current?.focus()
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Slash menu keyboard navigation
    if (slashMenuOpen && filteredCommands.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSlashSelectedIdx((prev) => Math.min(prev + 1, filteredCommands.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSlashSelectedIdx((prev) => Math.max(prev - 1, 0))
        return
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
        e.preventDefault()
        const selected = filteredCommands[slashSelectedIdx]
        if (selected) {
          handleSlashSelect(selected)
        }
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setSlashMenuOpen(false)
        return
      }
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const handleSend = () => {
    const text = value.trim()
    if (!text || disabled) return
    setValue('')
    setSlashMenuOpen(false)
    if (textareaRef.current) {
      textareaRef.current.style.height = `${LINE_HEIGHT}px`
    }
    onSend(text)
  }

  const hasText = value.trim().length > 0
  const charCount = value.length

  return (
    <div
      style={{
        flexShrink: 0,
        padding: '0 0 0.75rem',
        background: 'var(--bg)',
        borderTop: '1px solid var(--border)',
      }}
    >
      {/* Input field styled per design: $ prefix + textarea + send button */}
      <div
        style={{
          position: 'relative',
          display: 'flex',
          alignItems: 'flex-start',
          gap: 8,
          border: `1px solid ${focused ? 'var(--border-focus)' : 'var(--border)'}`,
          borderRadius: 0,
          background: 'var(--panel, var(--input-bg))',
          padding: '14px 18px',
          transition: 'border-color 0.15s',
        }}
      >
        {/* Slash command menu overlay */}
        <SlashCommandMenu
          commands={slashCommands}
          filter={slashFilter}
          selectedIndex={slashSelectedIdx}
          onSelect={handleSlashSelect}
          visible={slashMenuOpen}
        />

        {/* $ prompt prefix */}
        <span
          style={{
            color: 'var(--primary)',
            fontFamily: "'JetBrains Mono', 'Fira Mono', monospace",
            fontSize: 14,
            fontWeight: 700,
            lineHeight: `${LINE_HEIGHT}px`,
            userSelect: 'none',
            flexShrink: 0,
            paddingBottom: 1,
          }}
        >
          $
        </span>

        <textarea
          ref={textareaRef}
          value={value}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder="type a message..."
          rows={1}
          maxLength={MAX_CHARS}
          disabled={disabled}
          style={{
            flex: 1,
            border: 'none',
            outline: 'none',
            resize: 'none',
            background: 'transparent',
            color: 'var(--fg)',
            fontSize: 14,
            lineHeight: `${LINE_HEIGHT}px`,
            height: LINE_HEIGHT,
            maxHeight: MAX_TEXTAREA_HEIGHT,
            overflowY: 'auto',
            fontFamily: 'inherit',
            padding: 0,
            margin: 0,
          }}
        />

        {/* Action buttons: while streaming, Send still queues feedback (steering) alongside Stop */}
        {streaming && (
          <button
            onClick={onCancel}
            title="Stop generation"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 32,
              height: 32,
              borderRadius: 0,
              border: 'none',
              background: 'var(--error)',
              color: '#fff',
              cursor: 'pointer',
              flexShrink: 0,
              transition: 'opacity 0.15s',
            }}
          >
            <FontAwesomeIcon icon={faStop} style={{ fontSize: 12 }} />
          </button>
        )}
        <button
          onClick={handleSend}
          disabled={!hasText || disabled}
          title={streaming ? 'Queue feedback' : 'Send message'}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 32,
            height: 32,
            borderRadius: 0,
            border: 'none',
            background: hasText && !disabled ? 'var(--primary)' : 'var(--surface)',
            color: hasText && !disabled ? 'var(--primary-fg)' : 'var(--fg-dim)',
            cursor: hasText && !disabled ? 'pointer' : 'default',
            flexShrink: 0,
            transition: 'background 0.15s, color 0.15s',
            boxShadow: 'none',
          }}
        >
          <FontAwesomeIcon icon={faPaperPlane} style={{ fontSize: 12 }} />
        </button>
      </div>

      {/* Footer hints + char counter */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          marginTop: '0.3rem',
          padding: '0 0.125rem',
          fontSize: 11,
          color: 'var(--fg-dim)',
        }}
      >
        <span>
          {streaming
            ? 'Agent running · Enter queues feedback, injected at the next safe boundary'
            : goalActive
              ? 'Type / for commands'
              : 'Enter to send · / for commands · Shift+Enter for newline'}
        </span>
        <span style={{ color: charCount > MAX_CHARS * 0.9 ? 'var(--warning)' : 'var(--fg-dim)' }}>
          {charCount.toLocaleString()} / {MAX_CHARS.toLocaleString()}
        </span>
      </div>
    </div>
  )
}
