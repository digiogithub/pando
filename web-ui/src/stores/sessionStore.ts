import { create } from 'zustand'
import type { Session, Message } from '@/types'
import api from '@/services/api'
import { mapSession, mapMessages } from '@/services/mappers'

interface SessionStore {
  sessions: Session[]
  activeSessionId: string | null
  messages: Message[]
  loading: boolean
  /** true while the active session has a live background run streaming in */
  isStreaming: boolean
  fetchSessions: () => Promise<void>
  setActiveSession: (id: string) => Promise<{ isRunning: boolean }>
  setMessages: (msgs: Message[]) => void
  addMessage: (msg: Message) => void
  updateLastMessage: (content: string) => void
  updateLastMessageParts: (parts: import('@/types').ContentPart[]) => void
  markSessionRunning: (id: string, running: boolean) => void
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
    set({ activeSessionId: id, messages: [] })
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
}))
