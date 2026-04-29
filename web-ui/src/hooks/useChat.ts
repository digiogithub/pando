import { useState, useRef, useCallback } from 'react'
import { createSSEStream } from '@/services/sse'
import { useSessionStore } from '@/stores/sessionStore'
import type {
  Message, SSEEvent, SSEToolCall, SSEToolResult, SSEToolCallUpdate,
  ContentPart, ToolKind, ToolCallStatus, ToolCallLocation, SSEPlanEntry,
} from '@/types'

export interface ActiveToolCall {
  id: string
  name: string
  input: string
  kind?: ToolKind
  title?: string
  status: ToolCallStatus
  locations?: ToolCallLocation[]
  result?: SSEToolResult
  is_error?: boolean
  diff?: {
    file_path: string
    old_string?: string
    new_string?: string
    new_content?: string
  }
  terminal?: {
    terminal_id: string
    exit_code: number
  }
}

export interface PlanEntry {
  title: string
  status: string
  active_form?: string
}

export interface StreamingState {
  thinking: string
  toolCalls: ActiveToolCall[]
  plan: PlanEntry[]
}

interface UseChatOptions {
  onNewSession?: (sessionId: string) => void
  onDone?: () => void
}

export function useChat({ onNewSession, onDone }: UseChatOptions = {}) {
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [streamingState, setStreamingState] = useState<StreamingState>({ thinking: '', toolCalls: [], plan: [] })
  const abortRef = useRef<AbortController | null>(null)
  const { activeSessionId, addMessage, updateLastMessage, updateLastMessageParts, fetchSessions } = useSessionStore()

  const sendMessage = useCallback(
    async (text: string) => {
      if (!text.trim() || streaming) return
      setError(null)
      setStreaming(true)
      setStreamingState({ thinking: '', toolCalls: [], plan: [] })

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
      let thinkingAccum = ''
      let toolCallsAccum: ActiveToolCall[] = []
      let planAccum: PlanEntry[] = []

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
            thinkingAccum += event.content
            setStreamingState((prev) => ({ ...prev, thinking: prev.thinking + event.content! }))
          }

          // ── Tool call start (rich metadata from backend)
          if (event.type === 'tool_call' && event.tool_call) {
            const tc: SSEToolCall = event.tool_call
            const existing = toolCallsAccum.find((t) => t.id === tc.id)
            if (existing) {
              // Update existing entry with latest data
              toolCallsAccum = toolCallsAccum.map((t) =>
                t.id === tc.id ? {
                  ...t,
                  input: tc.input,
                  kind: tc.kind ?? t.kind,
                  title: tc.title ?? t.title,
                  status: tc.status ?? t.status,
                  locations: tc.locations ?? t.locations,
                } : t,
              )
              setStreamingState((prev) => ({
                ...prev,
                toolCalls: prev.toolCalls.map((t) =>
                  t.id === tc.id ? {
                    ...t,
                    input: tc.input,
                    kind: tc.kind ?? t.kind,
                    title: tc.title ?? t.title,
                    status: tc.status ?? t.status,
                    locations: tc.locations ?? t.locations,
                  } : t,
                ),
              }))
            } else {
              const newTc: ActiveToolCall = {
                id: tc.id,
                name: tc.name,
                input: tc.input,
                kind: tc.kind,
                title: tc.title,
                status: tc.status ?? 'pending',
                locations: tc.locations,
              }
              toolCallsAccum = [...toolCallsAccum, newTc]
              setStreamingState((prev) => ({
                ...prev,
                toolCalls: [...prev.toolCalls, newTc],
              }))
            }
          }

          // ── Tool call update (status/input change mid-execution)
          if (event.type === 'tool_call_update' && event.tool_call_update) {
            const upd: SSEToolCallUpdate = event.tool_call_update
            toolCallsAccum = toolCallsAccum.map((tc) =>
              tc.id === upd.id ? {
                ...tc,
                status: upd.status ?? tc.status,
                kind: upd.kind ?? tc.kind,
                title: upd.title ?? tc.title,
                input: upd.input ?? tc.input,
                locations: upd.locations ?? tc.locations,
              } : tc,
            )
            setStreamingState((prev) => ({
              ...prev,
              toolCalls: prev.toolCalls.map((tc) =>
                tc.id === upd.id ? {
                  ...tc,
                  status: upd.status ?? tc.status,
                  kind: upd.kind ?? tc.kind,
                  title: upd.title ?? tc.title,
                  input: upd.input ?? tc.input,
                  locations: upd.locations ?? tc.locations,
                } : tc,
              ),
            }))
          }

          // ── Tool result (rich metadata: status, diff, terminal, locations)
          if (event.type === 'tool_result' && event.tool_result) {
            const tr: SSEToolResult = event.tool_result
            toolCallsAccum = toolCallsAccum.map((tc) =>
              tc.id === tr.tool_call_id ? {
                ...tc,
                result: tr,
                is_error: tr.is_error,
                status: tr.status ?? (tr.is_error ? 'failed' : 'completed'),
                kind: tr.kind ?? tc.kind,
                title: tr.title ?? tc.title,
                locations: tr.locations ?? tc.locations,
                diff: tr.diff,
                terminal: tr.terminal,
              } : tc,
            )
            setStreamingState((prev) => ({
              ...prev,
              toolCalls: prev.toolCalls.map((tc) =>
                tc.id === tr.tool_call_id
                  ? {
                      ...tc,
                      result: tr,
                      is_error: tr.is_error,
                      status: tr.status ?? (tr.is_error ? 'failed' : 'completed'),
                      kind: tr.kind ?? tc.kind,
                      title: tr.title ?? tc.title,
                      locations: tr.locations ?? tc.locations,
                      diff: tr.diff,
                      terminal: tr.terminal,
                    }
                  : tc,
              ),
            }))
          }

          // ── Plan updates
          if (event.type === 'plan_update' && event.plan_entries) {
            planAccum = event.plan_entries.map((e: SSEPlanEntry) => ({
              title: e.title,
              status: e.status,
              active_form: e.active_form,
            }))
            setStreamingState((prev) => ({ ...prev, plan: planAccum }))
          }

          if (event.type === 'error') {
            setError(event.error ?? 'Unknown error')
          }
        },
        (err) => {
          setError(err.message)
          setStreaming(false)
          setStreamingState({ thinking: '', toolCalls: [], plan: [] })
        },
        () => {
          // Build final content parts and persist to message before clearing state
          const parts: ContentPart[] = []
          if (thinkingAccum) {
            parts.push({ type: 'reasoning', text: thinkingAccum })
          }
          for (const tc of toolCallsAccum) {
            let parsedInput: Record<string, unknown> | undefined
            try { parsedInput = JSON.parse(tc.input) } catch { parsedInput = undefined }
            parts.push({
              type: 'tool_call',
              tool_name: tc.name,
              tool_call_id: tc.id,
              tool_input: parsedInput,
              tool_result: tc.result?.content,
              is_error: tc.is_error,
            })
          }
          if (accumulated) {
            parts.push({ type: 'text', text: accumulated })
          }
          if (parts.length > 0) {
            updateLastMessageParts(parts)
          }
          setStreaming(false)
          setStreamingState({ thinking: '', toolCalls: [], plan: [] })
          fetchSessions()
          onDone?.()
        },
      )
    },
    [activeSessionId, streaming, addMessage, updateLastMessage, updateLastMessageParts, fetchSessions, onNewSession, onDone],
  )

  const cancelStreaming = useCallback(() => {
    abortRef.current?.abort()
    setStreaming(false)
    setStreamingState({ thinking: '', toolCalls: [], plan: [] })
  }, [])

  return { sendMessage, streaming, error, cancelStreaming, streamingState }
}
