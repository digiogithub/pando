import { useEffect } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircle, faLeaf } from '@fortawesome/free-solid-svg-icons'
import { useChat } from '@/hooks/useChat'
import { useSessionStore } from '@/stores/sessionStore'
import { useServerStore } from '@/stores/serverStore'
import { authenticate } from '@/services/auth'
import MessageList from './MessageList'
import ChatInput from './ChatInput'

export default function SimpleChatView() {
  const { messages, fetchSessions, sessions, activeSessionId } = useSessionStore()
  const { connected, startHealthCheck, setConnected } = useServerStore()
  const { sendMessage, streaming, error, cancelStreaming } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
  })

  const activeSession = sessions.find((s) => s.id === activeSessionId)

  // Initialize auth + health check on standalone mount
  useEffect(() => {
    authenticate()
      .then(() => {
        fetchSessions()
        setConnected(true)
      })
      .catch(() => setConnected(false))

    const stop = startHealthCheck()
    return stop
  }, [])

  const totalTokens = activeSession
    ? activeSession.prompt_tokens + activeSession.completion_tokens
    : 0

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100vh',
        background: 'var(--bg)',
        color: 'var(--fg)',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          padding: '0 1.25rem',
          height: 48,
          background: 'var(--sidebar-bg)',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
        }}
      >
        <FontAwesomeIcon icon={faLeaf} style={{ color: 'var(--primary)', fontSize: 18 }} />
        <span style={{ fontWeight: 700, fontSize: 16, letterSpacing: '-0.02em' }}>Pando</span>

        {activeSession && (
          <>
            <span style={{ color: 'var(--border)', fontSize: 16, userSelect: 'none' }}>·</span>
            <span
              style={{
                fontSize: 13,
                color: 'var(--fg-muted)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                maxWidth: 300,
              }}
            >
              {activeSession.title || `Session ${activeSession.id.slice(0, 8)}`}
            </span>
          </>
        )}
      </div>

      {/* Message area — centered */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
        <div
          style={{
            flex: 1,
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            maxWidth: 900,
            width: '100%',
            margin: '0 auto',
            alignSelf: 'stretch',
          }}
        >
          <MessageList messages={messages} streaming={streaming} />
        </div>

        {/* Error banner */}
        {error && (
          <div
            style={{
              maxWidth: 900,
              width: '100%',
              margin: '0 auto',
              padding: '0 1rem',
            }}
          >
            <div
              style={{
                marginBottom: '0.5rem',
                padding: '0.5rem 0.75rem',
                background: 'var(--error)',
                color: 'white',
                borderRadius: 'var(--radius-sm)',
                fontSize: 13,
              }}
            >
              {error}
            </div>
          </div>
        )}

        {/* Input — centered */}
        <div
          style={{
            maxWidth: 900,
            width: '100%',
            margin: '0 auto',
          }}
        >
          <ChatInput onSend={sendMessage} streaming={streaming} onCancel={cancelStreaming} />
        </div>
      </div>

      {/* Footer status bar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          height: 28,
          padding: '0 1rem',
          background: 'var(--bg-secondary)',
          borderTop: '1px solid var(--border)',
          fontSize: 11,
          color: 'var(--fg-muted)',
          flexShrink: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <FontAwesomeIcon
            icon={faCircle}
            style={{ fontSize: 7, color: connected ? 'var(--success)' : 'var(--error)' }}
          />
          <span>{connected ? 'Connected' : 'Disconnected'}</span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          {activeSession && totalTokens > 0 && (
            <>
              <span>{activeSession.message_count} messages</span>
              <span>·</span>
              <span>{totalTokens.toLocaleString()} tokens</span>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
