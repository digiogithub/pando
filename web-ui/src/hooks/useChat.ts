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

export type StreamItem =
  | { type: 'text'; text: string }
  | { type: 'tool'; id: string }
  | { type: 'thinking'; text: string }

export interface StreamingState {
  thinking: string
  toolCalls: ActiveToolCall[]
  plan: PlanEntry[]
  items: StreamItem[]
}

interface UseChatOptions {
  onNewSession?: (sessionId: string) => void
  onDone?: () => void
}

export function useChat({ onNewSession, onDone }: UseChatOptions = {}) {
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [streamingState, setStreamingState] = useState<StreamingState>({ thinking: '', toolCalls: [], plan: [], items: [] })
  const abortRef = useRef<AbortController | null>(null)
  const { activeSessionId, addMessage, updateLastMessage, updateLastMessageParts, fetchSessions } = useSessionStore()

  const sendMessage = useCallback(
    async (text: string) => {
      if (!text.trim() || streaming) return
      setError(null)
      setStreaming(true)
      setStreamingState({ thinking: '', toolCalls: [], plan: [], items: [] })

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
      // Ordered sequence of content items as they arrive
      const itemsAccum: StreamItem[] = []

      abortRef.current = createSSEStream(
        '/api/v1/chat/stream',
        { sessionId: activeSessionId ?? undefined, prompt: text },
        (event: SSEEvent) => {
          if (event.type === 'session' && event.session_id) {
            onNewSession?.(event.session_id)
          }
          if (event.type === 'content_delta' && event.content) {
            accumulated += event.content
            // Append to last text item or create a new one
            const last = itemsAccum[itemsAccum.length - 1]
            if (last?.type === 'text') {
              last.text += event.content
            } else {
              itemsAccum.push({ type: 'text', text: event.content })
            }
            setStreamingState((prev) => {
              const newItems = [...prev.items]
              const lastItem = newItems[newItems.length - 1]
              if (lastItem?.type === 'text') {
                newItems[newItems.length - 1] = { type: 'text', text: lastItem.text + event.content! }
              } else {
                newItems.push({ type: 'text', text: event.content! })
              }
              return { ...prev, items: newItems }
            })
            updateLastMessage(accumulated)
          }
          // Legacy 'content' event fallback
          if (event.type === 'content' && event.content) {
            accumulated += event.content
            const last = itemsAccum[itemsAccum.length - 1]
            if (last?.type === 'text') {
              last.text += event.content
            } else {
              itemsAccum.push({ type: 'text', text: event.content })
            }
            setStreamingState((prev) => {
              const newItems = [...prev.items]
              const lastItem = newItems[newItems.length - 1]
              if (lastItem?.type === 'text') {
                newItems[newItems.length - 1] = { type: 'text', text: lastItem.text + event.content! }
              } else {
                newItems.push({ type: 'text', text: event.content! })
              }
              return { ...prev, items: newItems }
            })
            updateLastMessage(accumulated)
          }
          if (event.type === 'thinking_delta' && event.content) {
            thinkingAccum += event.content
            // Append to last thinking item or create a new one
            const last = itemsAccum[itemsAccum.length - 1]
            if (last?.type === 'thinking') {
              last.text += event.content
            } else {
              itemsAccum.push({ type: 'thinking', text: event.content })
            }
            setStreamingState((prev) => {
              const newItems = [...prev.items]
              const lastItem = newItems[newItems.length - 1]
              if (lastItem?.type === 'thinking') {
                newItems[newItems.length - 1] = { type: 'thinking', text: lastItem.text + event.content! }
              } else {
                newItems.push({ type: 'thinking', text: event.content! })
              }
              return { ...prev, thinking: prev.thinking + event.content!, items: newItems }
            })
          }

          // ── Tool call start (rich metadata from backend)
          if (event.type === 'tool_call' && event.tool_call) {
            const tc: SSEToolCall = event.tool_call
            const existing = toolCallsAccum.find((t) => t.id === tc.id)
            if (existing) {
              // Update existing entry with latest data (no new item in itemsAccum)
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
              // Add ordered item for this new tool call
              itemsAccum.push({ type: 'tool', id: tc.id })
              setStreamingState((prev) => ({
                ...prev,
                toolCalls: [...prev.toolCalls, newTc],
                items: [...prev.items, { type: 'tool', id: tc.id }],
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
          setStreamingState({ thinking: '', toolCalls: [], plan: [], items: [] })
        },
        () => {
          // Build final content parts in arrival order using itemsAccum
          const parts: ContentPart[] = []
          for (const item of itemsAccum) {
            if (item.type === 'thinking') {
              parts.push({ type: 'reasoning', text: item.text })
            } else if (item.type === 'text') {
              parts.push({ type: 'text', text: item.text })
            } else if (item.type === 'tool') {
              const tc = toolCallsAccum.find((t) => t.id === item.id)
              if (tc) {
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
            }
          }
          if (parts.length > 0) {
            updateLastMessageParts(parts)
          }
          setStreaming(false)
          setStreamingState({ thinking: '', toolCalls: [], plan: [], items: [] })
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
    setStreamingState({ thinking: '', toolCalls: [], plan: [], items: [] })
  }, [])

  return { sendMessage, streaming, error, cancelStreaming, streamingState }
}
