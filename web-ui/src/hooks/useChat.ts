import { useState, useRef, useCallback } from 'react'
import { createSSEStream } from '@/services/sse'
import { useSessionStore } from '@/stores/sessionStore'
import type { Message, SSEEvent, SSEToolCall, SSEToolResult, ContentPart } from '@/types'

export interface ActiveToolCall {
  id: string
  name: string
  input: string
  result?: SSEToolResult
  is_error?: boolean
}

export interface StreamingState {
  thinking: string
  toolCalls: ActiveToolCall[]
}

interface UseChatOptions {
  onNewSession?: (sessionId: string) => void
}

export function useChat({ onNewSession }: UseChatOptions = {}) {
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [streamingState, setStreamingState] = useState<StreamingState>({ thinking: '', toolCalls: [] })
  const abortRef = useRef<AbortController | null>(null)
  const { activeSessionId, addMessage, updateLastMessage, updateLastMessageParts, fetchSessions } = useSessionStore()

  const sendMessage = useCallback(
    async (text: string) => {
      if (!text.trim() || streaming) return
      setError(null)
      setStreaming(true)
      setStreamingState({ thinking: '', toolCalls: [] })

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
          if (event.type === 'content_delta' && event.content) {
            accumulated += event.content
            updateLastMessage(accumulated)
          }
          // Legacy 'content' event fallback
          if (event.type === 'content' && event.content) {
            accumulated += event.content
            updateLastMessage(accumulated)
          }
          if (event.type === 'thinking_delta' && event.content) {
            setStreamingState((prev) => ({ ...prev, thinking: prev.thinking + event.content! }))
          }
          if (event.type === 'tool_call' && event.tool_call) {
            const tc: SSEToolCall = event.tool_call
            setStreamingState((prev) => ({
              ...prev,
              toolCalls: [
                ...prev.toolCalls,
                { id: tc.id, name: tc.name, input: tc.input },
              ],
            }))
          }
          if (event.type === 'tool_result' && event.tool_result) {
            const tr = event.tool_result
            setStreamingState((prev) => ({
              ...prev,
              toolCalls: prev.toolCalls.map((tc) =>
                tc.id === tr.tool_call_id
                  ? { ...tc, result: tr, is_error: tr.is_error }
                  : tc,
              ),
            }))
          }
          if (event.type === 'error') {
            setError(event.error ?? 'Unknown error')
          }
        },
        (err) => {
          setError(err.message)
          setStreaming(false)
          setStreamingState({ thinking: '', toolCalls: [] })
        },
        () => {
          setStreaming(false)
          setStreamingState({ thinking: '', toolCalls: [] })
          fetchSessions()
        },
      )
    },
    [activeSessionId, streaming, addMessage, updateLastMessage, fetchSessions, onNewSession],
  )

  const cancelStreaming = useCallback(() => {
    abortRef.current?.abort()
    setStreaming(false)
    setStreamingState({ thinking: '', toolCalls: [] })
  }, [])

  return { sendMessage, streaming, error, cancelStreaming, streamingState }
}
