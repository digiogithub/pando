import { useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlus } from '@fortawesome/free-solid-svg-icons'
import { useChat } from '@/hooks/useChat'
import { useDesktopNotifications } from '@/hooks/useDesktopNotifications'
import { useSessionStore } from '@/stores/sessionStore'
import { useLayoutStore } from '@/stores/layoutStore'
import MessageList from './MessageList'
import ChatInput from './ChatInput'

export default function ChatView() {
  const { t } = useTranslation()
  const { messages, fetchSessions, sessions, activeSessionId, setMessages } = useSessionStore()
  const { notify } = useDesktopNotifications()
  const sidebarOpen = useLayoutStore((s) => s.sidebarOpen)

  const handleDone = useCallback(() => {
    const session = sessions.find((s) => s.id === activeSessionId)
    const title = session?.title ?? t('chat.agentDoneTitle')
    notify(title, {
      body: t('chat.agentDoneBody'),
      onClick: () => {
        window.focus()
      },
      onlyWhenBackground: true,
    })
  }, [notify, sessions, activeSessionId, t])

  const { sendMessage, streaming, error, cancelStreaming, streamingState } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
    onDone: handleDone,
  })

  // Load sessions on mount if not already loaded
  useEffect(() => {
    void fetchSessions()
  }, [fetchSessions])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden', position: 'relative' }}>
      {/* New session FAB — visible only when sidebar is collapsed */}
      {!sidebarOpen && (
        <button
          title={t('nav.newSession')}
          onClick={() => {
            useSessionStore.setState({ activeSessionId: null })
            setMessages([])
          }}
          style={{
            position: 'absolute',
            top: '0.5rem',
            left: '0.5rem',
            zIndex: 10,
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            padding: '0.375rem 0.625rem',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            fontSize: 12,
            lineHeight: 1,
            boxShadow: '0 1px 4px rgba(0,0,0,0.15)',
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.color = 'var(--fg)'
            e.currentTarget.style.borderColor = 'var(--primary)'
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.color = 'var(--fg-muted)'
            e.currentTarget.style.borderColor = 'var(--border)'
          }}
        >
          <FontAwesomeIcon icon={faPlus} style={{ fontSize: 10 }} />
          {t('nav.newSession')}
        </button>
      )}
      <MessageList messages={messages} streaming={streaming} streamingState={streamingState} />

      {error && (
        <div
          style={{
            margin: '0 1rem 0.5rem',
            padding: '0.5rem 0.75rem',
            background: 'var(--error)',
            color: 'white',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      <ChatInput onSend={sendMessage} streaming={streaming} onCancel={cancelStreaming} />
    </div>
  )
}
