import { create } from 'zustand'
import api from '@/services/api'

export interface TerminalEntry {
  id: string
  type: 'command' | 'output' | 'error'
  text: string
  timestamp: string
}

interface TerminalStore {
  entries: TerminalEntry[]
  running: boolean
  history: string[]
  historyIndex: number
  addEntry: (e: Omit<TerminalEntry, 'id'>) => void
  clearEntries: () => void
  execCommand: (cmd: string) => Promise<void>
  setHistoryIndex: (i: number) => void
}

export const useTerminalStore = create<TerminalStore>((set, get) => ({
  entries: [],
  running: false,
  history: [],
  historyIndex: -1,

  addEntry: (e) =>
    set((s) => ({
      entries: [...s.entries, { ...e, id: `${Date.now()}-${Math.random()}` }],
    })),

  clearEntries: () => set({ entries: [] }),

  execCommand: async (cmd: string) => {
    if (!cmd.trim()) return
    const { addEntry } = get()

    // Add to history
    set((s) => ({ history: [cmd, ...s.history.slice(0, 99)], historyIndex: -1 }))

    addEntry({ type: 'command', text: cmd, timestamp: new Date().toISOString() })
    set({ running: true })

    try {
      const result = await api.post<{ output: string; error?: string }>(
        '/api/v1/terminal/exec',
        { command: cmd },
      )
      if (result.output) {
        addEntry({ type: 'output', text: result.output, timestamp: new Date().toISOString() })
      }
      if (result.error) {
        addEntry({ type: 'error', text: result.error, timestamp: new Date().toISOString() })
      }
    } catch (e) {
      addEntry({
        type: 'error',
        text: e instanceof Error ? e.message : 'Command failed',
        timestamp: new Date().toISOString(),
      })
    } finally {
      set({ running: false })
    }
  },

  setHistoryIndex: (historyIndex) => set({ historyIndex }),
}))
