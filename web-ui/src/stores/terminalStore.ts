import { create } from 'zustand'
import api from '@/services/api'
import { terminatePtyConnection, type PtyReadyInfo } from '@/services/terminalPty'

export interface TerminalEntry {
  id: string
  type: 'command' | 'output' | 'error'
  text: string
  timestamp: string
}

/**
 * 'pty' is the interactive path: a real shell over a websocket, rendered by
 * xterm.js. 'exec' is the legacy one-shot fallback (POST /terminal/exec) used
 * when the websocket cannot be established.
 */
export type TerminalMode = 'pty' | 'exec'

export interface TerminalTab {
  id: string
  title: string
  mode: TerminalMode
  /** Server-side PTY session id; lets a page reload reattach to the same shell. */
  ptySessionId?: string
  ptyExitCode?: number
  fallbackReason?: string
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
  attachPtySession: (tabId: string, info: PtyReadyInfo) => void
  markPtyExited: (tabId: string, code: number) => void
  fallbackToExec: (tabId: string, reason: string) => void
}

function makeId(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

function createDefaultTab(index: number): TerminalTab {
  return {
    id: makeId('terminal-tab'),
    title: `Terminal ${index}`,
    mode: 'pty',
    entries: [],
    running: false,
    history: [],
    historyIndex: -1,
  }
}

const PTY_TABS_KEY = 'pando_terminal_pty_tabs'

interface PersistedTab {
  id: string
  title: string
  ptySessionId?: string
}

/**
 * Tab identity and PTY session ids survive a page reload through sessionStorage,
 * which is what lets the terminal reattach to shells that are still running.
 * Scrollback is not persisted: the server replays it on reattach.
 */
function persistTabs(tabs: TerminalTab[]): void {
  try {
    const payload: PersistedTab[] = tabs
      .filter((tab) => tab.mode === 'pty' && tab.ptySessionId)
      .map((tab) => ({ id: tab.id, title: tab.title, ptySessionId: tab.ptySessionId }))
    sessionStorage.setItem(PTY_TABS_KEY, JSON.stringify(payload))
  } catch {
    // Private browsing or a full quota: reattach is a nicety, never fail on it.
  }
}

function loadPersistedTabs(): TerminalTab[] {
  try {
    const raw = sessionStorage.getItem(PTY_TABS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as PersistedTab[]
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((tab) => tab?.id && tab?.ptySessionId)
      .map((tab) => ({
        id: tab.id,
        title: tab.title || 'Terminal',
        mode: 'pty' as const,
        ptySessionId: tab.ptySessionId,
        entries: [],
        running: false,
        history: [],
        historyIndex: -1,
      }))
  } catch {
    return []
  }
}

function initialTabs(): TerminalTab[] {
  const restored = loadPersistedTabs()
  return restored.length > 0 ? restored : [createDefaultTab(1)]
}

function updateTab(tabs: TerminalTab[], tabId: string, updater: (tab: TerminalTab) => TerminalTab): TerminalTab[] {
  return tabs.map((tab) => (tab.id === tabId ? updater(tab) : tab))
}

export const useTerminalStore = create<TerminalStore>((set, get) => ({
  tabs: initialTabs(),
  activeTabId: null,

  createTab: () =>
    set((state) => {
      const tab = createDefaultTab(state.tabs.length + 1)
      return {
        tabs: [...state.tabs, tab],
        activeTabId: tab.id,
      }
    }),

  closeTab: (tabId) => {
    // Closing a tab is the one action that kills the shell; unmounting the pane
    // for any other reason only detaches, so the session can be reattached.
    terminatePtyConnection(tabId)

    set((state) => {
      if (state.tabs.length === 1) {
        const replacement = createDefaultTab(1)
        persistTabs([replacement])
        return { tabs: [replacement], activeTabId: replacement.id }
      }

      const nextTabs = state.tabs.filter((tab) => tab.id !== tabId)
      const activeTabId =
        state.activeTabId === tabId
          ? nextTabs[Math.max(0, state.tabs.findIndex((tab) => tab.id === tabId) - 1)]?.id ?? nextTabs[0]?.id ?? null
          : state.activeTabId
      persistTabs(nextTabs)
      return { tabs: nextTabs, activeTabId }
    })
  },

  attachPtySession: (tabId, info) =>
    set((state) => {
      const tabs = updateTab(state.tabs, tabId, (tab) => ({
        ...tab,
        mode: 'pty',
        ptySessionId: info.sessionId,
        shell: info.shell ?? tab.shell,
        cwd: info.dir ?? tab.cwd,
        ptyExitCode: undefined,
        fallbackReason: undefined,
      }))
      persistTabs(tabs)
      return { tabs }
    }),

  markPtyExited: (tabId, code) =>
    set((state) => {
      const tabs = updateTab(state.tabs, tabId, (tab) => ({
        ...tab,
        ptyExitCode: code,
        ptySessionId: undefined,
      }))
      persistTabs(tabs)
      return { tabs }
    }),

  fallbackToExec: (tabId, reason) =>
    set((state) => {
      const tabs = updateTab(state.tabs, tabId, (tab) => ({
        ...tab,
        mode: 'exec',
        ptySessionId: undefined,
        fallbackReason: reason,
      }))
      persistTabs(tabs)
      return { tabs }
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
