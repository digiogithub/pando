import { create } from 'zustand'
import type { Session, Message, PermissionRequest, PermissionAction, QuestionRequest, QuestionAnswer } from '../types'
import api from '../services/api'
import { mapSession, mapMessages } from '../services/mappers'

/** page size used by the session list; the API caps a request at 500 */
export const SESSIONS_PAGE_SIZE = 100

interface SessionStore {
  sessions: Session[]
  /** total sessions on the server (not only the loaded page) */
  sessionsTotal: number
  /** true when the server has more sessions past the loaded ones */
  sessionsHasMore: boolean
  /** true while a "load more" page is in flight */
  sessionsLoadingMore: boolean
  activeSessionId: string | null
  messages: Message[]
  loading: boolean
  /** true while the active session has a live background run streaming in */
  isStreaming: boolean
  /** pending tool permission prompts awaiting a user decision */
  pendingPermissions: PermissionRequest[]
  /** pending AskUserQuestion prompts awaiting a user answer */
  pendingQuestions: QuestionRequest[]
  /** whether the active session auto-approves tool permissions ("auto mode") */
  autoApprove: boolean
  fetchSessions: () => Promise<void>
  /** append the next page of sessions to the list */
  loadMoreSessions: () => Promise<void>
  setActiveSession: (id: string) => Promise<{ isRunning: boolean }>
  setMessages: (msgs: Message[]) => void
  addMessage: (msg: Message) => void
  updateLastMessage: (content: string) => void
  updateLastMessageParts: (parts: import('../types').ContentPart[]) => void
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
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type RawSessionDetail = { session: any; messages: any[]; is_running?: boolean }

export const useSessionStore = create<SessionStore>((set, get) => ({
  sessions: [],
  sessionsTotal: 0,
  sessionsHasMore: false,
  sessionsLoadingMore: false,
  activeSessionId: null,
  messages: [],
  loading: false,
  isStreaming: false,
  pendingPermissions: [],
  pendingQuestions: [],
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
    set({ activeSessionId: id, messages: [], pendingPermissions: [], pendingQuestions: [] })
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
      set({ messages, isStreaming: isRunning })

      // Reflect running status in the sessions list too
      set((s) => ({
        sessions: s.sessions.map((sess) =>
          sess.id === id ? { ...sess, is_running: isRunning } : sess
        ),
      }))

      return { isRunning }
    } catch {
      set({ messages: [], isStreaming: false })
      return { isRunning: false }
    }
  },

  setMessages: (messages) => set({ messages }),

  addMessage: (msg) =>
    set((s) => ({ messages: [...s.messages, msg] })),

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

  addPermissionRequest: (req) =>
    set((s) =>
      s.pendingPermissions.some((p) => p.id === req.id)
        ? s
        : { pendingPermissions: [...s.pendingPermissions, req] }
    ),

  respondPermission: async (id, sessionId, action) => {
    // Optimistically remove the prompt; the agent unblocks server-side.
    set((s) => ({ pendingPermissions: s.pendingPermissions.filter((p) => p.id !== id) }))
    try {
      await api.post('/api/v1/permissions/respond', { id, sessionId, action })
    } catch {
      // Network failure already surfaced by the api layer.
    }
  },

  addQuestionRequest: (req) =>
    set((s) =>
      s.pendingQuestions.some((q) => q.id === req.id)
        ? s
        : { pendingQuestions: [...s.pendingQuestions, req] }
    ),

  respondQuestion: async (id, sessionId, answers) => {
    // Optimistically remove the prompt; the agent unblocks server-side.
    set((s) => ({ pendingQuestions: s.pendingQuestions.filter((q) => q.id !== id) }))
    try {
      await api.post('/api/v1/questions/respond', { id, sessionId, answers })
    } catch {
      // Network failure already surfaced by the api layer.
    }
  },

  cancelQuestion: async (id, sessionId) => {
    set((s) => ({ pendingQuestions: s.pendingQuestions.filter((q) => q.id !== id) }))
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
      }>(`/api/v1/sessions/${sessionId}/pending`)
      set((s) => {
        if (s.activeSessionId !== sessionId) return s
        const perms = data.permissions ?? []
        const questions = data.questions ?? []
        const newPerms = perms.filter((p) => !s.pendingPermissions.some((x) => x.id === p.id))
        const newQuestions = questions.filter((q) => !s.pendingQuestions.some((x) => x.id === q.id))
        // The server owns the run state: a dropped SSE stream must not leave the
        // session marked as finished while the agent keeps working (or stays
        // blocked on a tool). This is what lets the UI reattach to the stream.
        const running = Boolean(data.running)
        const sessions = s.sessions.some((sess) => sess.id === sessionId && sess.is_running !== running)
          ? s.sessions.map((sess) => (sess.id === sessionId ? { ...sess, is_running: running } : sess))
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
