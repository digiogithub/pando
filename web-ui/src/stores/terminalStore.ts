import { create } from 'zustand'
import api from '@/services/api'

export interface TerminalEntry {
  id: string
  type: 'command' | 'output' | 'error'
  text: string
  timestamp: string
}

export interface TerminalTab {
  id: string
  title: string
  sessionId?: string
  entries: TerminalEntry[]
  running: boolean
  history: string[]
  historyIndex: number
  cwd?: string
  shell?: string
}

interface ExecResult {
  output: string
  error?: string
  exit_code: number
  session_id?: string
  shell?: string
  dir?: string
}

interface TerminalStore {
  tabs: TerminalTab[]
  activeTabId: string | null
  createTab: () => void
  closeTab: (tabId: string) => void
  setActiveTab: (tabId: string) => void
  clearEntries: (tabId?: string) => void
  execCommand: (cmd: string, tabId?: string) => Promise<void>
  setHistoryIndex: (historyIndex: number, tabId?: string) => void
}

function makeId(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

function createDefaultTab(index: number): TerminalTab {
  return {
    id: makeId('terminal-tab'),
    title: `Terminal ${index}`,
    entries: [],
    running: false,
    history: [],
    historyIndex: -1,
  }
}

function updateTab(tabs: TerminalTab[], tabId: string, updater: (tab: TerminalTab) => TerminalTab): TerminalTab[] {
  return tabs.map((tab) => (tab.id === tabId ? updater(tab) : tab))
}

export const useTerminalStore = create<TerminalStore>((set, get) => ({
  tabs: [createDefaultTab(1)],
  activeTabId: null,

  createTab: () =>
    set((state) => {
      const tab = createDefaultTab(state.tabs.length + 1)
      return {
        tabs: [...state.tabs, tab],
        activeTabId: tab.id,
      }
    }),

  closeTab: (tabId) =>
    set((state) => {
      if (state.tabs.length === 1) {
        const replacement = createDefaultTab(1)
        return { tabs: [replacement], activeTabId: replacement.id }
      }

      const nextTabs = state.tabs.filter((tab) => tab.id !== tabId)
      const activeTabId =
        state.activeTabId === tabId
          ? nextTabs[Math.max(0, state.tabs.findIndex((tab) => tab.id === tabId) - 1)]?.id ?? nextTabs[0]?.id ?? null
          : state.activeTabId
      return { tabs: nextTabs, activeTabId }
    }),

  setActiveTab: (tabId) => set({ activeTabId: tabId }),

  clearEntries: (tabId) => {
    const state = get()
    const resolvedTabId = tabId ?? state.activeTabId ?? state.tabs[0]?.id
    if (!resolvedTabId) return
    set((current) => ({
      tabs: updateTab(current.tabs, resolvedTabId, (tab) => ({ ...tab, entries: [] })),
    }))
  },

  execCommand: async (cmd, tabId) => {
    if (!cmd.trim()) return

    const state = get()
    const resolvedTabId = tabId ?? state.activeTabId ?? state.tabs[0]?.id
    if (!resolvedTabId) return

    const timestamp = new Date().toISOString()
    const commandEntry: TerminalEntry = {
      id: makeId('terminal-entry'),
      type: 'command',
      text: cmd,
      timestamp,
    }

    set((current) => ({
      tabs: updateTab(current.tabs, resolvedTabId, (tab) => ({
        ...tab,
        entries: [...tab.entries, commandEntry],
        history: [cmd, ...tab.history.filter((item) => item !== cmd).slice(0, 99)],
        historyIndex: -1,
        running: true,
      })),
    }))

    const currentTab = get().tabs.find((tab) => tab.id === resolvedTabId)

    try {
      const result = await api.post<ExecResult>('/api/v1/terminal/exec', {
        command: cmd,
        session_id: currentTab?.sessionId,
      })

      set((current) => ({
        tabs: updateTab(current.tabs, resolvedTabId, (tab) => {
          const nextEntries = [...tab.entries]
          if (result.output) {
            nextEntries.push({
              id: makeId('terminal-entry'),
              type: result.exit_code === 0 ? 'output' : 'error',
              text: result.output,
              timestamp: new Date().toISOString(),
            })
          }
          if (result.error) {
            nextEntries.push({
              id: makeId('terminal-entry'),
              type: 'error',
              text: result.error,
              timestamp: new Date().toISOString(),
            })
          }

          return {
            ...tab,
            entries: nextEntries,
            running: false,
            sessionId: result.session_id ?? tab.sessionId,
            shell: result.shell ?? tab.shell,
            cwd: result.dir ?? tab.cwd,
          }
        }),
      }))
    } catch (error) {
      set((current) => ({
        tabs: updateTab(current.tabs, resolvedTabId, (tab) => ({
          ...tab,
          entries: [
            ...tab.entries,
            {
              id: makeId('terminal-entry'),
              type: 'error',
              text: error instanceof Error ? error.message : 'Command failed',
              timestamp: new Date().toISOString(),
            },
          ],
          running: false,
        })),
      }))
    }
  },

  setHistoryIndex: (historyIndex, tabId) => {
    const state = get()
    const resolvedTabId = tabId ?? state.activeTabId ?? state.tabs[0]?.id
    if (!resolvedTabId) return
    set((current) => ({
      tabs: updateTab(current.tabs, resolvedTabId, (tab) => ({ ...tab, historyIndex })),
    }))
  },
}))
