import { useEffect, useRef } from 'react'
import type { Message } from '@/types'
import type { StreamingState } from '@/hooks/useChat'
import MessageBubble, { EventRow } from './MessageBubble'
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

  // If nothing yet, show simple spinner
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

  // Show event rows for live thinking + tool calls
  return (
    <div style={{ display: 'flex', flexDirection: 'column', padding: '0.375rem 0' }}>
      {hasThinking && (
        <EventRow kind="thinking" thinking={streamingState.thinking} isLive />
      )}
      {streamingState.toolCalls.map((tc) => (
        <EventRow
          key={tc.id}
          kind="tool"
          toolName={tc.name}
          toolInput={(() => { try { return JSON.parse(tc.input) } catch { return null } })()}
          toolResult={tc.result?.content}
          isError={tc.is_error}
          isLive={tc.status === 'pending' || tc.status === 'in_progress'}
          backendTitle={tc.title}
          backendKind={tc.kind}
          toolStatus={tc.status}
          locations={tc.locations}
          diff={tc.diff}
          terminal={tc.terminal}
        />
      ))}
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
  // Only show LoadingBubble when there is no assistant message yet.
  // useChat always adds an empty assistant message before starting the SSE stream,
  // so MessageBubble handles all live state (spinner, thinking, tool calls).
  // Showing LoadingBubble when text === '' causes duplicate tool-call rows.
  const showLoadingBubble =
    streaming && (!lastMessage || lastMessage.role !== 'assistant')

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
