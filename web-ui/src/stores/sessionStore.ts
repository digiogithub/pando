import { create } from 'zustand'
import type { Session, Message, PermissionRequest, PermissionAction } from '@/types'
import api from '@/services/api'
import { mapSession, mapMessages } from '@/services/mappers'

interface SessionStore {
  sessions: Session[]
  activeSessionId: string | null
  messages: Message[]
  loading: boolean
  /** true while the active session has a live background run streaming in */
  isStreaming: boolean
  /** pending tool permission prompts awaiting a user decision */
  pendingPermissions: PermissionRequest[]
  /** whether the active session auto-approves tool permissions ("auto mode") */
  autoApprove: boolean
  fetchSessions: () => Promise<void>
  setActiveSession: (id: string) => Promise<{ isRunning: boolean }>
  setMessages: (msgs: Message[]) => void
  addMessage: (msg: Message) => void
  updateLastMessage: (content: string) => void
  updateLastMessageParts: (parts: import('@/types').ContentPart[]) => void
  markSessionRunning: (id: string, running: boolean) => void
  /** apply a live (possibly estimated) context-window token update to a session */
  updateSessionTokens: (
    id: string,
    promptTokens: number,
    completionTokens: number,
    contextWindow: number,
    estimated: boolean,
  ) => void
  // Permission / auto-mode handling
  addPermissionRequest: (req: PermissionRequest) => void
  respondPermission: (id: string, sessionId: string, action: PermissionAction) => Promise<void>
  fetchAutoApprove: (sessionId: string) => Promise<void>
  setAutoApprove: (sessionId: string, enabled: boolean) => Promise<void>
  toggleAutoApprove: (sessionId: string) => Promise<void>
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type RawSessions = { sessions: any[] }
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type RawSessionDetail = { session: any; messages: any[]; is_running?: boolean }

export const useSessionStore = create<SessionStore>((set) => ({
  sessions: [],
  activeSessionId: null,
  messages: [],
  loading: false,
  isStreaming: false,
  pendingPermissions: [],
  autoApprove: false,

  fetchSessions: async () => {
    set({ loading: true })
    try {
      const data = await api.get<RawSessions>('/api/v1/sessions')
      const sessions = (data.sessions ?? []).map(mapSession)
      set({ sessions })
    } finally {
      set({ loading: false })
    }
  },

  setActiveSession: async (id: string) => {
    set({ activeSessionId: id, messages: [], pendingPermissions: [] })
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

  updateSessionTokens: (id, promptTokens, completionTokens, contextWindow, estimated) =>
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.id === id
          ? {
              ...sess,
              prompt_tokens: promptTokens,
              completion_tokens: completionTokens,
              context_window: contextWindow > 0 ? contextWindow : sess.context_window,
              tokens_estimated: estimated,
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
