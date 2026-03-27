import { useState, useRef, useCallback } from 'react'
import { createSSEStream } from '@/services/sse'
import { useSessionStore } from '@/stores/sessionStore'
import type { Message, SSEEvent } from '@/types'

interface UseChatOptions {
  onNewSession?: (sessionId: string) => void
}

export function useChat({ onNewSession }: UseChatOptions = {}) {
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const { activeSessionId, addMessage, updateLastMessage, fetchSessions } = useSessionStore()

  const sendMessage = useCallback(
    async (text: string) => {
      if (!text.trim() || streaming) return
      setError(null)
      setStreaming(true)

      const userMsg: Message = {
        id: `tmp-user-${Date.now()}`,
        session_id: activeSessionId ?? '',
        role: 'user',
        content: [{ type: 'text', text }],
        created_at: new Date().toISOString(),
      }
      addMessage(userMsg)

      const assistantMsg: Message = {
        id: `tmp-asst-${Date.now()}`,
        session_id: activeSessionId ?? '',
        role: 'assistant',
        content: [{ type: 'text', text: '' }],
        created_at: new Date().toISOString(),
      }
      addMessage(assistantMsg)

      let accumulated = ''

      abortRef.current = createSSEStream(
        '/api/v1/chat/stream',
        { sessionId: activeSessionId ?? undefined, prompt: text },
        (event: SSEEvent) => {
          if (event.type === 'session' && event.session_id) {
            onNewSession?.(event.session_id)
          }
          if (event.type === 'content' && event.content) {
            accumulated += event.content
            updateLastMessage(accumulated)
          }
          if (event.type === 'error') {
            setError(event.error ?? 'Unknown error')
          }
        },
        (err) => {
          setError(err.message)
          setStreaming(false)
        },
        () => {
          setStreaming(false)
          fetchSessions()
        },
      )
    },
    [activeSessionId, streaming, addMessage, updateLastMessage, fetchSessions, onNewSession],
  )

  const cancelStreaming = useCallback(() => {
    abortRef.current?.abort()
    setStreaming(false)
  }, [])

  return { sendMessage, streaming, error, cancelStreaming }
}
