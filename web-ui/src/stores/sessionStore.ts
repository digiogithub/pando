import { create } from 'zustand'
import type { Session, Message } from '@/types'
import api from '@/services/api'

interface SessionStore {
  sessions: Session[]
  activeSessionId: string | null
  messages: Message[]
  loading: boolean
  fetchSessions: () => Promise<void>
  setActiveSession: (id: string) => Promise<void>
  setMessages: (msgs: Message[]) => void
  addMessage: (msg: Message) => void
  updateLastMessage: (content: string) => void
}

export const useSessionStore = create<SessionStore>((set, get) => ({
  sessions: [],
  activeSessionId: null,
  messages: [],
  loading: false,

  fetchSessions: async () => {
    set({ loading: true })
    try {
      const data = await api.get<{ sessions: Session[] }>('/api/v1/sessions')
      set({ sessions: data.sessions ?? [] })
    } finally {
      set({ loading: false })
    }
  },

  setActiveSession: async (id: string) => {
    set({ activeSessionId: id, messages: [] })
    try {
      const data = await api.get<{ session: Session; messages: Message[] }>(`/api/v1/sessions/${id}`)
      set({ messages: data.messages ?? [] })
    } catch {
      set({ messages: [] })
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
}))
