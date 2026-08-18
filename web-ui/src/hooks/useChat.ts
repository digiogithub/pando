import { useState, useRef, useCallback } from 'react'
import { createSSEStream, createGETSSEStream } from '@/services/sse'
import { api } from '@/services/api'
import { useSessionStore } from '@/stores/sessionStore'
import { useFileChangesStore } from '@/stores/fileChangesStore'
import type {
  Message, SSEEvent, SSEToolCall, SSEToolResult, SSEToolCallUpdate,
  ContentPart, ToolKind, ToolCallStatus, ToolCallLocation, SSEPlanEntry, GoalStatus,
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
  goal: GoalStatus | null
}

/**
 * Outcome of queueing mid-run feedback: 'queued' (accepted by the agent loop),
 * 'not_busy' (no active run — the caller should start a normal run instead),
 * or 'error'.
 */
export type SteerResult = 'queued' | 'not_busy' | 'error'

interface UseChatOptions {
  onNewSession?: (sessionId: string) => void
  onDone?: () => void
  onEvent?: (event: SSEEvent) => void
  onCancelled?: (sessionId: string | null) => Promise<void> | void
}

/** Build a fresh empty StreamingState */
function emptyState(): StreamingState {
  return { thinking: '', toolCalls: [], plan: [], items: [], goal: null }
}

export function useChat({ onNewSession, onDone, onEvent, onCancelled }: UseChatOptions = {}) {
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [streamingState, setStreamingState] = useState<StreamingState>(emptyState())
  const abortRef = useRef<AbortController | null>(null)
  const { activeSessionId, addMessage, updateLastMessage, updateLastMessageParts, fetchSessions, markSessionRunning } = useSessionStore()

  // Accumulated state shared between sendMessage and reconnectSession.
  // These refs live for the lifetime of the hook instance.
  const accumulatedRef = useRef('')
  const toolCallsRef = useRef<ActiveToolCall[]>([])
  const planRef = useRef<PlanEntry[]>([])
  const itemsRef = useRef<StreamItem[]>([])
  const goalRef = useRef<GoalStatus | null>(null)
  // Mid-run feedback the user submitted while the agent was busy but that the
  // agent loop has not injected yet. Rendered as "queued" chips until the
  // backend emits steering_injected, at which point they become real user turns.
  const pendingSteerRef = useRef<string[]>([])
  const [pendingFeedback, setPendingFeedback] = useState<string[]>([])

  const resetAccum = useCallback(() => {
    accumulatedRef.current = ''
    toolCallsRef.current = []
    planRef.current = []
    itemsRef.current = []
    goalRef.current = null
    useFileChangesStore.getState().clearChanges()
  }, [])

  /**
   * Materialize the accumulated stream (text, thinking, tool calls) as message
   * content parts. Used both when the run ends and when a steering message
   * splits the live assistant bubble into a finished one plus a fresh one.
   */
  const buildStreamParts = useCallback((): ContentPart[] => {
    const parts: ContentPart[] = []
    for (const item of itemsRef.current) {
      if (item.type === 'thinking') {
        parts.push({ type: 'reasoning', text: item.text })
      } else if (item.type === 'text') {
        parts.push({ type: 'text', text: item.text })
      } else if (item.type === 'tool') {
        const tc = toolCallsRef.current.find((t) => t.id === item.id)
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
    return parts
  }, [])

  /** Process a single SSE event and update React state. */
  const handleEvent = useCallback(
    (event: SSEEvent, sessionIdForNewSession?: string) => {
      onEvent?.(event)

      if (event.type === 'session' && event.session_id) {
        onNewSession?.(event.session_id)
        if (sessionIdForNewSession == null) {
          // sendMessage path: the session was just created
        }
      }

      if (event.type === 'goal_status') {
        goalRef.current = event.goal ?? null
        setStreamingState((prev) => ({ ...prev, goal: event.goal ?? null }))
      }

      if (event.type === 'token_usage' && event.token_usage) {
        const sid = event.session_id ?? activeSessionId
        if (sid) {
          const tu = event.token_usage
          useSessionStore.getState().updateSessionTokens(
            sid,
            tu.prompt_tokens,
            tu.completion_tokens,
            tu.context_window,
            tu.estimated,
            {
              cacheReadTokens: tu.cache_read_tokens,
              cacheCreationTokens: tu.cache_creation_tokens,
              reasoningTokens: tu.reasoning_tokens,
              cost: tu.cost,
            },
          )
        }
      }

      if (event.type === 'permission_request' && event.permission_request) {
        useSessionStore.getState().addPermissionRequest(event.permission_request)
      }

      if (event.type === 'question_request' && event.question_request) {
        useSessionStore.getState().addQuestionRequest(event.question_request)
      }

      if (event.type === 'content_delta' && event.content) {
        accumulatedRef.current += event.content
        const last = itemsRef.current[itemsRef.current.length - 1]
        if (last?.type === 'text') {
          last.text += event.content
        } else {
          itemsRef.current.push({ type: 'text', text: event.content })
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
        updateLastMessage(accumulatedRef.current)
      }

      // Legacy 'content' event fallback
      if (event.type === 'content' && event.content) {
        accumulatedRef.current += event.content
        const last = itemsRef.current[itemsRef.current.length - 1]
        if (last?.type === 'text') {
          last.text += event.content
        } else {
          itemsRef.current.push({ type: 'text', text: event.content })
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
        updateLastMessage(accumulatedRef.current)
      }

      if (event.type === 'thinking_delta' && event.content) {
        const last = itemsRef.current[itemsRef.current.length - 1]
        if (last?.type === 'thinking') {
          last.text += event.content
        } else {
          itemsRef.current.push({ type: 'thinking', text: event.content })
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

      if (event.type === 'tool_call' && event.tool_call) {
        const tc: SSEToolCall = event.tool_call
        const existing = toolCallsRef.current.find((t) => t.id === tc.id)
        if (existing) {
          toolCallsRef.current = toolCallsRef.current.map((t) =>
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
          toolCallsRef.current = [...toolCallsRef.current, newTc]
          itemsRef.current.push({ type: 'tool', id: tc.id })
          setStreamingState((prev) => ({
            ...prev,
            toolCalls: [...prev.toolCalls, newTc],
            items: [...prev.items, { type: 'tool', id: tc.id }],
          }))
        }
      }

      if (event.type === 'tool_call_update' && event.tool_call_update) {
        const upd: SSEToolCallUpdate = event.tool_call_update
        toolCallsRef.current = toolCallsRef.current.map((tc) =>
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

      if (event.type === 'tool_result' && event.tool_result) {
        const tr: SSEToolResult = event.tool_result
        toolCallsRef.current = toolCallsRef.current.map((tc) =>
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

        // Track file changes for the FileChangesBar
        if (tr.diff && !tr.is_error) {
          const meta = tr.raw_output?.metadata as { additions?: number; removals?: number } | undefined
          const oldStr = tr.diff.old_string ?? ''
          const newStr = tr.diff.new_string ?? tr.diff.new_content ?? ''
          // Use backend metadata if available, otherwise estimate from content
          const additions = meta?.additions ?? (newStr ? newStr.split('\n').length : 0)
          const removals = meta?.removals ?? (oldStr ? oldStr.split('\n').length : 0)
          useFileChangesStore.getState().addChange(
            tr.diff.file_path,
            additions,
            removals,
            oldStr,
            newStr,
          )
        }
      }

      if (event.type === 'plan_update' && event.plan_entries) {
        planRef.current = event.plan_entries.map((e: SSEPlanEntry) => ({
          title: e.title,
          status: e.status,
          active_form: e.active_form,
        }))
        setStreamingState((prev) => ({ ...prev, plan: planRef.current }))
      }

      // The agent loop reached a safe boundary and turned the queued feedback
      // into real user turns. Close the assistant bubble that was streaming,
      // render the feedback as its own user message(s), and open a fresh
      // assistant bubble so subsequent deltas do not overwrite the user turn.
      if (event.type === 'steering_injected' && pendingSteerRef.current.length > 0) {
        const queued = pendingSteerRef.current
        pendingSteerRef.current = []
        setPendingFeedback([])

        const finishedParts = buildStreamParts()
        if (finishedParts.length > 0) {
          updateLastMessageParts(finishedParts)
        }

        const sid = event.session_id ?? activeSessionId ?? ''
        const now = Date.now()
        queued.forEach((text, idx) => {
          addMessage({
            id: `steer-${now}-${idx}`,
            session_id: sid,
            role: 'user',
            content: [{ type: 'text', text }],
            created_at: new Date().toISOString(),
          })
        })
        addMessage({
          id: `tmp-asst-steer-${now}`,
          session_id: sid,
          role: 'assistant',
          content: [{ type: 'text', text: '' }],
          created_at: new Date().toISOString(),
        })

        // Start a clean accumulation window for the new assistant bubble; the
        // previous one already holds its finished parts.
        accumulatedRef.current = ''
        itemsRef.current = []
        toolCallsRef.current = []
        setStreamingState((prev) => ({ ...prev, thinking: '', toolCalls: [], items: [] }))
      }

      if (event.type === 'error') {
        setError(event.error ?? 'Unknown error')
      }
    },
    [onEvent, onNewSession, updateLastMessage, updateLastMessageParts, addMessage, buildStreamParts, activeSessionId],
  )

  /** Called when the stream ends (done event or connection closed). */
  const handleDone = useCallback(
    (sessionId: string | null) => {
      const parts = buildStreamParts()
      if (parts.length > 0) {
        updateLastMessageParts(parts)
      }
      // Anything still queued was never injected (run ended first); drop the
      // chips so they do not linger over a finished conversation.
      pendingSteerRef.current = []
      setPendingFeedback([])
      setStreaming(false)
      setStreamingState(emptyState())
      if (sessionId) markSessionRunning(sessionId, false)
      fetchSessions()
      onDone?.()
    },
    [buildStreamParts, updateLastMessageParts, fetchSessions, markSessionRunning, onDone],
  )

  /**
   * Queue mid-run feedback for the active session. The message is injected into
   * the agent loop at the next safe boundary without cancelling the run. Used
   * when the user submits while a run is already streaming.
   */
  const steer = useCallback(
    async (text: string): Promise<SteerResult> => {
      const sessionId = activeSessionId
      if (!text.trim() || !sessionId) return 'error'

      // Show the feedback as "queued" right away. It becomes a real user turn
      // in the transcript only when the agent loop injects it (steering_injected).
      pendingSteerRef.current = [...pendingSteerRef.current, text]
      setPendingFeedback(pendingSteerRef.current)

      const unqueue = () => {
        pendingSteerRef.current = pendingSteerRef.current.filter((t) => t !== text)
        setPendingFeedback(pendingSteerRef.current)
      }

      try {
        await api.post(`/api/v1/sessions/${sessionId}/steer`, { prompt: text })
        return 'queued'
      } catch (err) {
        unqueue()
        const msg = err instanceof Error ? err.message : 'failed to queue feedback'
        // 409 not_busy: the run finished between the UI check and the request.
        // The caller falls back to starting a normal run so the text is not lost.
        if (msg.includes('not_busy')) return 'not_busy'
        setError(msg)
        return 'error'
      }
    },
    [activeSessionId],
  )

  const sendMessage = useCallback(
    async (text: string) => {
      if (!text.trim()) return
      // While a run is active, route the message as steering feedback instead of
      // rejecting it or starting a new run.
      if (streaming) {
        const result = await steer(text)
        // 'not_busy' means the run already ended — fall through and start a
        // normal run instead of silently dropping the message.
        if (result !== 'not_busy') return
        setStreaming(false)
      }
      setError(null)
      setStreaming(true)
      resetAccum()
      setStreamingState(emptyState())

      const sessionId = activeSessionId

      const userMsg: Message = {
        id: `tmp-user-${Date.now()}`,
        session_id: sessionId ?? '',
        role: 'user',
        content: [{ type: 'text', text }],
        created_at: new Date().toISOString(),
      }
      addMessage(userMsg)

      const assistantMsg: Message = {
        id: `tmp-asst-${Date.now()}`,
        session_id: sessionId ?? '',
        role: 'assistant',
        content: [{ type: 'text', text: '' }],
        created_at: new Date().toISOString(),
      }
      addMessage(assistantMsg)

      abortRef.current = createSSEStream(
        '/api/v1/chat/stream',
        { sessionId: sessionId ?? undefined, prompt: text },
        (event: SSEEvent) => handleEvent(event),
        (err) => {
          setError(err.message)
          setStreaming(false)
          setStreamingState(emptyState())
        },
        () => handleDone(sessionId),
      )
    },
    [activeSessionId, streaming, steer, addMessage, handleEvent, handleDone, resetAccum],
  )

  /**
   * Reconnect to a session that is already running in the background.
   * Replays the server-side event buffer then continues live.
   */
  const reconnectSession = useCallback(
    (sessionId: string) => {
      if (streaming) {
        abortRef.current?.abort()
      }

      setError(null)
      setStreaming(true)
      resetAccum()
      setStreamingState(emptyState())

      // Add a placeholder assistant message to render streamed content into.
      const assistantMsg: Message = {
        id: `tmp-asst-reconnect-${Date.now()}`,
        session_id: sessionId,
        role: 'assistant',
        content: [{ type: 'text', text: '' }],
        created_at: new Date().toISOString(),
      }
      addMessage(assistantMsg)

      const baseURL = (window as Window & { __PANDO_API_BASE__?: string }).__PANDO_API_BASE__ ?? ''
      abortRef.current = createGETSSEStream(
        `${baseURL}/api/v1/sessions/${sessionId}/stream`,
        (event: SSEEvent) => handleEvent(event),
        (err) => {
          setError(err.message)
          setStreaming(false)
          setStreamingState(emptyState())
        },
        () => handleDone(sessionId),
      )
    },
    [streaming, addMessage, handleEvent, handleDone, resetAccum],
  )

  const cancelStreaming = useCallback(async () => {
    const sessionId = activeSessionId
    abortRef.current?.abort()
    setStreaming(false)
    setStreamingState(emptyState())
    if (sessionId) {
      markSessionRunning(sessionId, false)
    }
    await onCancelled?.(sessionId)
  }, [activeSessionId, markSessionRunning, onCancelled])

  return { sendMessage, steer, reconnectSession, streaming, error, cancelStreaming, streamingState, pendingFeedback }
}
