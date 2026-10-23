import { useModelAutoModeStore } from './modelAutoModeStore'
import { usePersonaRoutingStore } from './personaRoutingStore'
import { create } from 'zustand'
import type { Session, Message, PermissionRequest, PermissionAction, QuestionRequest, QuestionAnswer } from '../types'
import api from '../services/api'
import { mapSession, mapMessages, type RawMessage, type RawSession } from '../services/mappers'
import { applyRawMessage, reconcileMessages, removeMessage, removeSession, upsertSession } from '../services/liveUpdates'
import { RUN_SEQ_STALE } from '../services/runSeq'

/** page size used by the session list; the API caps a request at 500 */
export const SESSIONS_PAGE_SIZE = 100

/** how many answered prompt ids are remembered to ignore late re-deliveries */
const RESOLVED_IDS_LIMIT = 200

function markResolved(ids: string[], id: string): string[] {
  if (ids.includes(id)) return ids
  return [...ids, id].slice(-RESOLVED_IDS_LIMIT)
}

interface SessionStore {
  sessions: Session[]
  /** total sessions on the server (not only the loaded page) */
  sessionsTotal: number
  /** true when the server has more sessions past the loaded ones */
  sessionsHasMore: boolean
  /** true while a "load more" page is in flight */
  sessionsLoadingMore: boolean
  activeSessionId: string | null
  /** last run_seq streamed or loaded per session (see services/runSeq) */
  seenRunSeq: Record<string, number>
  messages: Message[]
  loading: boolean
  /** true while the active session has a live background run streaming in */
  isStreaming: boolean
  /** pending tool permission prompts awaiting a user decision */
  pendingPermissions: PermissionRequest[]
  /** pending AskUserQuestion prompts awaiting a user answer */
  pendingQuestions: QuestionRequest[]
  /**
   * ids of permission/question prompts the user already answered. A poll that
   * was in flight, or an SSE replay, can deliver one again before the server
   * has dropped it; those must not reopen the dialog. Bounded, oldest first.
   */
  resolvedRequestIds: string[]
  /** whether the active session auto-approves tool permissions ("auto mode") */
  autoApprove: boolean
  fetchSessions: () => Promise<void>
  /** append the next page of sessions to the list */
  loadMoreSessions: () => Promise<void>
  setActiveSession: (id: string) => Promise<{ isRunning: boolean }>
  /**
   * re-read the active session's history and status and merge it in place
   * (unchanged messages keep their identity): the catch-up when following a
   * session another process (a subagent) is running
   */
  resyncActiveSession: (id: string) => Promise<void>
  /** upsert one pushed raw message (Go message.Message JSON) into the active transcript */
  applyPushedMessage: (sessionId: string, raw: RawMessage) => void
  /** drop a pushed deleted message from the active transcript */
  removePushedMessage: (sessionId: string, id: string) => void
  /** upsert a pushed session list item, keeping list order */
  upsertPushedSession: (raw: RawSession) => void
  /** remove a pushed deleted session from the list */
  removePushedSession: (id: string) => void
  setMessages: (msgs: Message[]) => void
  setSeenRunSeq: (id: string, seq: number) => void
  addMessage: (msg: Message) => void
  /** Insert before the trailing (streaming) message so updateLastMessage* keep targeting it. */
  insertBeforeLast: (msg: Message) => void
  updateLastMessage: (content: string) => void
  updateLastMessageParts: (parts: import('../types').ContentPart[]) => void
  /** set a session's title manually (PATCH /api/v1/sessions/{id}) */
  renameSession: (id: string, title: string) => Promise<void>
  markSessionRunning: (id: string, running: boolean) => void
  /** apply a live (possibly estimated) context-window token update to a session */
  updateSessionTokens: (
    id: string,
    promptTokens: number,
    completionTokens: number,
    contextWindow: number,
    estimated: boolean,
    detail?: {
      cacheReadTokens?: number
      cacheCreationTokens?: number
      reasoningTokens?: number
      cost?: number
    },
  ) => void
  /** replace a session's context window, e.g. right after switching its model */
  setSessionContextWindow: (id: string, contextWindow: number) => void
  // Permission / auto-mode handling
  addPermissionRequest: (req: PermissionRequest) => void
  respondPermission: (id: string, sessionId: string, action: PermissionAction) => Promise<void>
  // AskUserQuestion handling
  addQuestionRequest: (req: QuestionRequest) => void
  respondQuestion: (id: string, sessionId: string, answers: QuestionAnswer[]) => Promise<void>
  cancelQuestion: (id: string, sessionId: string) => Promise<void>
  /** poll the server for permission/question prompts blocking a session */
  fetchPendingRequests: (sessionId: string) => Promise<void>
  fetchAutoApprove: (sessionId: string) => Promise<void>
  setAutoApprove: (sessionId: string, enabled: boolean) => Promise<void>
  toggleAutoApprove: (sessionId: string) => Promise<void>
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type RawSessions = { sessions: any[]; total?: number; has_more?: boolean }
type RawSessionDetail = {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  session: any
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  messages: any[]
  is_running?: boolean
  run_seq?: number
  busy_elsewhere?: boolean
}

export const useSessionStore = create<SessionStore>((set, get) => ({
  sessions: [],
  sessionsTotal: 0,
  sessionsHasMore: false,
  sessionsLoadingMore: false,
  activeSessionId: null,
  seenRunSeq: {},
  messages: [],
  loading: false,
  isStreaming: false,
  pendingPermissions: [],
  pendingQuestions: [],
  resolvedRequestIds: [],
  autoApprove: false,

  fetchSessions: async () => {
    set({ loading: true })
    try {
      const data = await api.get<RawSessions>(`/api/v1/sessions?limit=${SESSIONS_PAGE_SIZE}&offset=0`)
      const sessions = (data.sessions ?? []).map(mapSession)
      set({
        sessions,
        sessionsTotal: data.total ?? sessions.length,
        sessionsHasMore: data.has_more ?? false,
      })
    } finally {
      set({ loading: false })
    }
  },

  loadMoreSessions: async () => {
    const { sessions, sessionsHasMore, sessionsLoadingMore } = get()
    if (!sessionsHasMore || sessionsLoadingMore) return
    set({ sessionsLoadingMore: true })
    try {
      const data = await api.get<RawSessions>(
        `/api/v1/sessions?limit=${SESSIONS_PAGE_SIZE}&offset=${sessions.length}`,
      )
      const page = (data.sessions ?? []).map(mapSession)
      // Guard against duplicates: a session updated between pages can shift.
      const known = new Set(sessions.map((s) => s.id))
      const merged = [...sessions, ...page.filter((s) => !known.has(s.id))]
      set({
        sessions: merged,
        sessionsTotal: data.total ?? merged.length,
        sessionsHasMore: data.has_more ?? false,
      })
    } finally {
      set({ sessionsLoadingMore: false })
    }
  },

  setActiveSession: async (id: string) => {
    // Forget the baseline until the history below is loaded: a poll must not
    // compare against a sequence from an earlier visit to this session.
    set((s) => {
      const seenRunSeq = { ...s.seenRunSeq }
      delete seenRunSeq[id]
      return { activeSessionId: id, seenRunSeq, messages: [], pendingPermissions: [], pendingQuestions: [] }
    })
    useModelAutoModeStore.getState().setLastRoutedModel(null)
    usePersonaRoutingStore.getState().setApplied(null)
    // Load the session's auto-approve state (best effort).
    void (async () => {
      try {
        const res = await api.get<{ enabled: boolean }>(`/api/v1/sessions/${id}/auto-approve`)
        set({ autoApprove: Boolean(res.enabled) })
      } catch {
        set({ autoApprove: false })
      }
    })()
    try {
      const data = await api.get<RawSessionDetail>(`/api/v1/sessions/${id}`)
      const messages = mapMessages(data.messages ?? [])
      const isRunning = data.is_running ?? false
      const runSeq = data.run_seq ?? 0
      const busyElsewhere = data.busy_elsewhere ?? false
      // History already contains every finished run. A run still in flight is
      // not in it (or only partly), so leave the baseline stale and let the
      // reattach effect replay it.
      set((s) => ({
        messages,
        isStreaming: isRunning,
        seenRunSeq: { ...s.seenRunSeq, [id]: isRunning ? RUN_SEQ_STALE : runSeq },
        // Reflect running status and sequence in the sessions list too
        sessions: s.sessions.map((sess) =>
          sess.id === id ? { ...sess, is_running: isRunning, run_seq: runSeq, busy_elsewhere: busyElsewhere } : sess
        ),
      }))

      return { isRunning }
    } catch {
      set({ messages: [], isStreaming: false })
      return { isRunning: false }
    }
  },

  resyncActiveSession: async (id) => {
    try {
      const data = await api.get<RawSessionDetail>(`/api/v1/sessions/${id}`)
      set((s) => {
        if (s.activeSessionId !== id) return s
        const messages = reconcileMessages(s.messages, mapMessages(data.messages ?? []))
        // Only the run flags: the list's display title (derived server-side
        // for placeholder titles) must not be replaced by the raw one.
        const busyElsewhere = data.busy_elsewhere ?? false
        const isRunning = data.is_running ?? false
        const sessions = s.sessions.some(
          (sess) => sess.id === id && (Boolean(sess.busy_elsewhere) !== busyElsewhere || Boolean(sess.is_running) !== isRunning),
        )
          ? s.sessions.map((sess) => (sess.id === id ? { ...sess, busy_elsewhere: busyElsewhere, is_running: isRunning } : sess))
          : s.sessions
        if (messages === s.messages && sessions === s.sessions) return s
        return { messages, sessions }
      })
    } catch {
      // Best effort: the next push or poll catches up.
    }
  },

  applyPushedMessage: (sessionId, raw) =>
    set((s) => {
      if (s.activeSessionId !== sessionId) return s
      const messages = applyRawMessage(s.messages, raw)
      return messages === s.messages ? s : { messages }
    }),

  removePushedMessage: (sessionId, id) =>
    set((s) => {
      if (s.activeSessionId !== sessionId) return s
      const messages = removeMessage(s.messages, id)
      return messages === s.messages ? s : { messages }
    }),

  upsertPushedSession: (raw) =>
    set((s) => {
      const { sessions, added } = upsertSession(s.sessions, raw)
      if (sessions === s.sessions) return s
      return { sessions, sessionsTotal: added ? s.sessionsTotal + 1 : s.sessionsTotal }
    }),

  removePushedSession: (id) =>
    set((s) => {
      const { sessions, removed } = removeSession(s.sessions, id)
      if (!removed) return s
      return { sessions, sessionsTotal: Math.max(0, s.sessionsTotal - 1) }
    }),

  setMessages: (messages) => set({ messages }),

  setSeenRunSeq: (id, seq) => set((s) => ({ seenRunSeq: { ...s.seenRunSeq, [id]: seq } })),

  addMessage: (msg) =>
    set((s) => ({ messages: [...s.messages, msg] })),

  insertBeforeLast: (msg) =>
    set((s) => {
      if (s.messages.length === 0) return { messages: [msg] }
      const msgs = [...s.messages]
      msgs.splice(msgs.length - 1, 0, msg)
      return { messages: msgs }
    }),

  updateLastMessage: (content) =>
    set((s) => {
      const msgs = [...s.messages]
      if (msgs.length === 0) return s
      const last = { ...msgs[msgs.length - 1] }
      last.content = [{ type: 'text', text: content }]
      msgs[msgs.length - 1] = last
      return { messages: msgs }
    }),

  updateLastMessageParts: (parts) =>
    set((s) => {
      const msgs = [...s.messages]
      if (msgs.length === 0) return s
      const last = { ...msgs[msgs.length - 1] }
      last.content = parts
      msgs[msgs.length - 1] = last
      return { messages: msgs }
    }),

  renameSession: async (id, title) => {
    const trimmed = title.trim()
    if (!trimmed) return
    await api.patch(`/api/v1/sessions/${encodeURIComponent(id)}`, { title: trimmed })
    set((s) => ({
      sessions: s.sessions.map((sess) => (sess.id === id ? { ...sess, title: trimmed } : sess)),
    }))
  },

  markSessionRunning: (id, running) =>
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.id === id ? { ...sess, is_running: running } : sess
      ),
      isStreaming: s.activeSessionId === id ? running : s.isStreaming,
    })),

  updateSessionTokens: (id, promptTokens, completionTokens, contextWindow, estimated, detail) =>
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.id === id
          ? {
              ...sess,
              prompt_tokens: promptTokens,
              completion_tokens: completionTokens,
              context_window: contextWindow > 0 ? contextWindow : sess.context_window,
              tokens_estimated: estimated,
              cache_read_tokens: detail?.cacheReadTokens ?? sess.cache_read_tokens,
              cache_creation_tokens: detail?.cacheCreationTokens ?? sess.cache_creation_tokens,
              reasoning_tokens: detail?.reasoningTokens ?? sess.reasoning_tokens,
              cost: detail?.cost ?? sess.cost,
            }
          : sess
      ),
    })),

  setSessionContextWindow: (id, contextWindow) => {
    if (!(contextWindow > 0)) return
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.id === id ? { ...sess, context_window: contextWindow } : sess
      ),
    }))
  },

  addPermissionRequest: (req) =>
    set((s) =>
      s.pendingPermissions.some((p) => p.id === req.id) || s.resolvedRequestIds.includes(req.id)
        ? s
        : { pendingPermissions: [...s.pendingPermissions, req] }
    ),

  respondPermission: async (id, sessionId, action) => {
    // Optimistically remove the prompt; the agent unblocks server-side.
    set((s) => ({
      pendingPermissions: s.pendingPermissions.filter((p) => p.id !== id),
      resolvedRequestIds: markResolved(s.resolvedRequestIds, id),
    }))
    try {
      await api.post('/api/v1/permissions/respond', { id, sessionId, action })
    } catch {
      // Network failure already surfaced by the api layer.
    }
  },

  addQuestionRequest: (req) =>
    set((s) =>
      s.pendingQuestions.some((q) => q.id === req.id) || s.resolvedRequestIds.includes(req.id)
        ? s
        : { pendingQuestions: [...s.pendingQuestions, req] }
    ),

  respondQuestion: async (id, sessionId, answers) => {
    // Optimistically remove the prompt; the agent unblocks server-side.
    set((s) => ({
      pendingQuestions: s.pendingQuestions.filter((q) => q.id !== id),
      resolvedRequestIds: markResolved(s.resolvedRequestIds, id),
    }))
    try {
      await api.post('/api/v1/questions/respond', { id, sessionId, answers })
    } catch {
      // Network failure already surfaced by the api layer.
    }
  },

  cancelQuestion: async (id, sessionId) => {
    set((s) => ({
      pendingQuestions: s.pendingQuestions.filter((q) => q.id !== id),
      resolvedRequestIds: markResolved(s.resolvedRequestIds, id),
    }))
    try {
      await api.post('/api/v1/questions/respond', { id, sessionId, cancelled: true })
    } catch {
      // Network failure already surfaced by the api layer.
    }
  },

  // Pending prompts are pushed over the chat SSE stream, but a client that is not
  // attached to it (reload, dropped connection, run continued in the background)
  // would never render them while the agent stays blocked inside the tool. Polling
  // this endpoint makes an AskUserQuestion/permission prompt visible regardless of
  // the stream.
  fetchPendingRequests: async (sessionId) => {
    try {
      const data = await api.get<{
        permissions?: PermissionRequest[]
        questions?: QuestionRequest[]
        running?: boolean
        run_seq?: number
        busy_elsewhere?: boolean
      }>(`/api/v1/sessions/${sessionId}/pending`)
      set((s) => {
        if (s.activeSessionId !== sessionId) return s
        const perms = data.permissions ?? []
        const questions = data.questions ?? []
        const isNew = (id: string) => !s.resolvedRequestIds.includes(id)
        const newPerms = perms.filter((p) => isNew(p.id) && !s.pendingPermissions.some((x) => x.id === p.id))
        const newQuestions = questions.filter((q) => isNew(q.id) && !s.pendingQuestions.some((x) => x.id === q.id))
        // The server owns the run state: a dropped SSE stream must not leave the
        // session marked as finished while the agent keeps working (or stays
        // blocked on a tool). This is what lets the UI reattach to the stream.
        const running = Boolean(data.running)
        const runSeq = data.run_seq
        // Another process (a subagent) running the session keeps it read-only.
        const busyElsewhere = Boolean(data.busy_elsewhere)
        const changed = s.sessions.some(
          (sess) => sess.id === sessionId && (
            sess.is_running !== running
            || Boolean(sess.busy_elsewhere) !== busyElsewhere
            || (runSeq !== undefined && sess.run_seq !== runSeq)
          ),
        )
        const sessions = changed
          ? s.sessions.map((sess) =>
              sess.id === sessionId
                ? { ...sess, is_running: running, run_seq: runSeq ?? sess.run_seq, busy_elsewhere: busyElsewhere }
                : sess,
            )
          : s.sessions
        if (newPerms.length === 0 && newQuestions.length === 0 && sessions === s.sessions) return s
        return {
          pendingPermissions: [...s.pendingPermissions, ...newPerms],
          pendingQuestions: [...s.pendingQuestions, ...newQuestions],
          sessions,
        }
      })
    } catch {
      // Best effort: the api layer already surfaces network failures.
    }
  },

  fetchAutoApprove: async (sessionId) => {
    try {
      const res = await api.get<{ enabled: boolean }>(`/api/v1/sessions/${sessionId}/auto-approve`)
      set({ autoApprove: Boolean(res.enabled) })
    } catch {
      // ignore
    }
  },

  setAutoApprove: async (sessionId, enabled) => {
    set({ autoApprove: enabled })
    try {
      await api.post(`/api/v1/sessions/${sessionId}/auto-approve`, { enabled })
    } catch {
      // Revert on failure.
      set({ autoApprove: !enabled })
    }
  },

  toggleAutoApprove: async (sessionId) => {
    const next = !useSessionStore.getState().autoApprove
    await useSessionStore.getState().setAutoApprove(sessionId, next)
  },
}))
