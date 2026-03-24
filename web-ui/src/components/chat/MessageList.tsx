import { useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faComments } from '@fortawesome/free-solid-svg-icons'
import type { Message } from '@/types'
import MessageBubble from './MessageBubble'
import LoadingSpinner from '@/components/shared/LoadingSpinner'

// ─── Empty state ──────────────────────────────────────────────────────────────

function EmptyState() {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: '0.75rem',
        color: 'var(--fg-dim)',
        padding: '2rem',
        userSelect: 'none',
      }}
    >
      <FontAwesomeIcon icon={faComments} style={{ fontSize: 40, opacity: 0.35 }} />
      <p style={{ fontSize: 15, fontWeight: 500, color: 'var(--fg-muted)' }}>
        Start a conversation
      </p>
      <p style={{ fontSize: 13, textAlign: 'center', maxWidth: 320 }}>
        Type a message below and press Enter to chat with Pando.
      </p>
    </div>
  )
}

// ─── Loading bubble ───────────────────────────────────────────────────────────

function LoadingBubble() {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.625rem',
        padding: '0.5rem 1rem',
      }}
    >
      {/* Spacer matching avatar width */}
      <div style={{ width: 32, height: 32, flexShrink: 0 }} />
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm) var(--radius-md) var(--radius-md) var(--radius-md)',
          padding: '0.625rem 0.875rem',
        }}
      >
        <LoadingSpinner size={14} />
        <span style={{ fontSize: 13, color: 'var(--fg-muted)' }}>Thinking…</span>
      </div>
    </div>
  )
}

// ─── Main component ───────────────────────────────────────────────────────────

interface MessageListProps {
  messages: Message[]
  streaming: boolean
}

export default function MessageList({ messages, streaming }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null)

  // Auto-scroll when messages change or last message content updates
  const lastContent = messages[messages.length - 1]?.content[0]?.text ?? ''
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length, lastContent])

  if (messages.length === 0 && !streaming) {
    return <EmptyState />
  }

  const lastMessage = messages[messages.length - 1]
  const showLoadingBubble =
    streaming && (!lastMessage || lastMessage.role !== 'assistant' || lastMessage.content[0]?.text === '')

  return (
    <div
      style={{
        flex: 1,
        overflowY: 'auto',
        overflowX: 'hidden',
        display: 'flex',
        flexDirection: 'column',
        paddingTop: '0.5rem',
        paddingBottom: '0.5rem',
      }}
    >
      {messages.map((msg, i) => (
        <MessageBubble
          key={msg.id}
          message={msg}
          streaming={streaming && i === messages.length - 1 && msg.role === 'assistant'}
        />
      ))}

      {showLoadingBubble && <LoadingBubble />}

      <div ref={bottomRef} />
    </div>
  )
}
