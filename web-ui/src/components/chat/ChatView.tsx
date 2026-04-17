import { useEffect } from 'react'
import { useChat } from '@/hooks/useChat'
import { useSessionStore } from '@/stores/sessionStore'
import MessageList from './MessageList'
import ChatInput from './ChatInput'

export default function ChatView() {
  const { messages, fetchSessions } = useSessionStore()
  const { sendMessage, streaming, error, cancelStreaming, streamingState } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
  })

  // Load sessions on mount if not already loaded
  useEffect(() => {
    void fetchSessions()
  }, [fetchSessions])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
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
