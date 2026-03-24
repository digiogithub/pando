import { create } from 'zustand'
import type { LogEntry } from '@/types'
import api from '@/services/api'

export type LogLevel = 'all' | 'debug' | 'info' | 'warn' | 'error'

interface LogsStore {
  entries: LogEntry[]
  selectedEntry: LogEntry | null
  levelFilter: LogLevel
  searchQuery: string
  autoScroll: boolean
  loading: boolean
  setLevelFilter: (level: LogLevel) => void
  setSearchQuery: (q: string) => void
  setSelectedEntry: (e: LogEntry | null) => void
  setAutoScroll: (v: boolean) => void
  fetchLogs: () => Promise<void>
  addEntry: (e: LogEntry) => void
}

export const useLogsStore = create<LogsStore>((set) => ({
  entries: [],
  selectedEntry: null,
  levelFilter: 'all',
  searchQuery: '',
  autoScroll: true,
  loading: false,

  setLevelFilter: (levelFilter) => set({ levelFilter }),
  setSearchQuery: (searchQuery) => set({ searchQuery }),
  setSelectedEntry: (selectedEntry) => set({ selectedEntry }),
  setAutoScroll: (autoScroll) => set({ autoScroll }),

  fetchLogs: async () => {
    set({ loading: true })
    try {
      const entries = await api.get<LogEntry[]>('/api/v1/logs?limit=200')
      set({ entries: entries ?? [] })
    } catch {
      set({ entries: [] })
    } finally {
      set({ loading: false })
    }
  },

  addEntry: (e) => set((s) => ({ entries: [...s.entries.slice(-500), e] })),
}))
