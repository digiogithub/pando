import { useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faComments } from '@fortawesome/free-solid-svg-icons'
import type { Message } from '@/types'
import type { StreamingState } from '@/hooks/useChat'
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

// ─── Loading bubble (shown when no content yet) ───────────────────────────────

function LoadingBubble({ streamingState }: { streamingState: StreamingState }) {
  const hasThinking = streamingState.thinking.length > 0
  const hasTools = streamingState.toolCalls.length > 0

  if (!hasThinking && !hasTools) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.625rem',
          padding: '0.5rem 1rem',
        }}
      >
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

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: '0.625rem',
        padding: '0.5rem 1rem',
      }}
    >
      <div style={{ width: 32, height: 32, flexShrink: 0 }} />
      <div style={{ maxWidth: 'min(680px, 80%)', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        {hasThinking && (
          <ThinkingBlock text={streamingState.thinking} live />
        )}
        {streamingState.toolCalls.map((tc) => (
          <LiveToolCallBlock key={tc.id} toolCall={tc} />
        ))}
      </div>
    </div>
  )
}

// ─── Live thinking block ──────────────────────────────────────────────────────

function ThinkingBlock({ text, live }: { text: string; live?: boolean }) {
  return (
    <div
      style={{
        border: '1px solid color-mix(in srgb, var(--primary) 30%, transparent)',
        borderRadius: 'var(--radius-sm)',
        overflow: 'hidden',
        fontSize: 12,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.375rem 0.625rem',
          background: 'color-mix(in srgb, var(--primary) 8%, var(--surface))',
          color: 'var(--primary)',
        }}
      >
        {live && <LoadingSpinner size={11} />}
        <span style={{ fontWeight: 600, fontSize: 11, letterSpacing: '0.04em', textTransform: 'uppercase' }}>
          Thinking
        </span>
      </div>
      <div
        style={{
          padding: '0.5rem 0.625rem',
          background: 'var(--bg-secondary)',
          color: 'var(--fg-muted)',
          fontFamily: 'monospace',
          fontSize: 11,
          lineHeight: 1.55,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          maxHeight: 220,
          overflowY: 'auto',
        }}
      >
        {text}
      </div>
    </div>
  )
}

// ─── Live tool call block ─────────────────────────────────────────────────────

function LiveToolCallBlock({ toolCall }: { toolCall: import('@/hooks/useChat').ActiveToolCall }) {
  const isDone = toolCall.result !== undefined
  const isError = toolCall.is_error

  return (
    <div
      style={{
        border: `1px solid ${isError ? 'var(--error)' : isDone ? 'var(--success)' : 'var(--border)'}33`,
        borderRadius: 'var(--radius-sm)',
        overflow: 'hidden',
        fontSize: 12,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.375rem 0.625rem',
          background: isError
            ? 'color-mix(in srgb, var(--error) 8%, var(--surface))'
            : isDone
              ? 'color-mix(in srgb, var(--success) 8%, var(--surface))'
              : 'var(--surface)',
          color: isError ? 'var(--error)' : isDone ? 'var(--success)' : 'var(--fg-muted)',
        }}
      >
        {!isDone && <LoadingSpinner size={11} />}
        <span style={{ fontFamily: 'monospace', fontWeight: 500 }}>{toolCall.name}</span>
        {isDone && (
          <span style={{ fontSize: 10, marginLeft: 'auto' }}>
            {isError ? '✗ error' : '✓ done'}
          </span>
        )}
      </div>
      {toolCall.input && (
        <div
          style={{
            padding: '0.375rem 0.625rem',
            background: 'var(--bg-secondary)',
            borderTop: '1px solid var(--border)',
            color: 'var(--fg-muted)',
            fontFamily: 'monospace',
            fontSize: 11,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all',
          }}
        >
          {(() => {
            try {
              return JSON.stringify(JSON.parse(toolCall.input), null, 2)
            } catch {
              return toolCall.input
            }
          })()}
        </div>
      )}
    </div>
  )
}

// ─── Main component ───────────────────────────────────────────────────────────

interface MessageListProps {
  messages: Message[]
  streaming: boolean
  streamingState: StreamingState
}

export default function MessageList({ messages, streaming, streamingState }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null)

  // Auto-scroll when messages change or last message content updates
  const lastContent = messages[messages.length - 1]?.content[0]?.text ?? ''
  const thinkingLen = streamingState.thinking.length
  const toolCount = streamingState.toolCalls.length
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length, lastContent, thinkingLen, toolCount])

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
          streamingState={i === messages.length - 1 && msg.role === 'assistant' ? streamingState : undefined}
        />
      ))}

      {showLoadingBubble && <LoadingBubble streamingState={streamingState} />}

      <div ref={bottomRef} />
    </div>
  )
}

export { ThinkingBlock, LiveToolCallBlock }
