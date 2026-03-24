import { useRef, useState, useCallback, type KeyboardEvent, type ChangeEvent } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPaperPlane, faStop } from '@fortawesome/free-solid-svg-icons'

const MAX_CHARS = 8000
const MAX_HEIGHT = 200

interface ChatInputProps {
  onSend: (text: string) => void
  streaming: boolean
  onCancel: () => void
  disabled?: boolean
}

export default function ChatInput({ onSend, streaming, onCancel, disabled }: ChatInputProps) {
  const [value, setValue] = useState('')
  const [focused, setFocused] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Auto-resize textarea
  const resize = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`
  }, [])

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    setValue(e.target.value)
    resize()
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const handleSend = () => {
    const text = value.trim()
    if (!text || streaming || disabled) return
    setValue('')
    // Reset height after clearing
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
    }
    onSend(text)
  }

  const hasText = value.trim().length > 0
  const charCount = value.length

  return (
    <div
      style={{
        flexShrink: 0,
        padding: '0.625rem 1rem 0.75rem',
        background: 'var(--bg)',
        borderTop: '1px solid var(--border)',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          gap: '0.5rem',
          border: `1.5px solid ${focused ? 'var(--border-focus)' : 'var(--border)'}`,
          borderRadius: 'var(--radius-md)',
          background: 'var(--input-bg)',
          padding: '0.5rem 0.625rem 0.5rem 0.875rem',
          transition: 'border-color 0.15s',
        }}
      >
        <textarea
          ref={textareaRef}
          value={value}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder="Message Pando…"
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
            lineHeight: 1.5,
            maxHeight: MAX_HEIGHT,
            overflowY: 'auto',
            fontFamily: 'inherit',
          }}
        />

        {/* Action button */}
        {streaming ? (
          <button
            onClick={onCancel}
            title="Stop generation"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 32,
              height: 32,
              borderRadius: 'var(--radius-sm)',
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
        ) : (
          <button
            onClick={handleSend}
            disabled={!hasText || disabled}
            title="Send message"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 32,
              height: 32,
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: hasText && !disabled ? 'var(--primary)' : 'var(--surface)',
              color: hasText && !disabled ? 'var(--primary-fg)' : 'var(--fg-dim)',
              cursor: hasText && !disabled ? 'pointer' : 'default',
              flexShrink: 0,
              transition: 'background 0.15s, color 0.15s',
            }}
          >
            <FontAwesomeIcon icon={faPaperPlane} style={{ fontSize: 12 }} />
          </button>
        )}
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
        <span>Enter to send · Shift+Enter for newline</span>
        <span style={{ color: charCount > MAX_CHARS * 0.9 ? 'var(--warning)' : 'var(--fg-dim)' }}>
          {charCount.toLocaleString()} / {MAX_CHARS.toLocaleString()}
        </span>
      </div>
    </div>
  )
}
