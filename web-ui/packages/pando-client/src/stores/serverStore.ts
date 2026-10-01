import { create } from 'zustand'
import { fetchServerInfo, isProjectChildMode, registerNetworkErrorHandler } from '../services/api'

// Polling interval when connected (relaxed) vs disconnected (fast reconnect detection).
const POLL_CONNECTED_MS = 30_000
const POLL_DISCONNECTED_MS = 5_000

interface ServerStore {
  connected: boolean
  version: string
  startupMode: string
  projectName: string
  projectId: string
  setConnected: (v: boolean) => void
  setVersion: (v: string) => void
  setServerInfo: (info: { version?: string; startupMode?: string; projectName?: string; projectId?: string }) => void
  startHealthCheck: () => () => void
}

export const useServerStore = create<ServerStore>((set, get) => ({
  connected: false,
  version: '',
  startupMode: isProjectChildMode() ? 'project-child' : 'unknown',
  projectName: '',
  projectId: '',
  setConnected: (connected) => set({ connected }),
  setVersion: (version) => set({ version }),
  setServerInfo: ({ version, startupMode, projectName, projectId }) =>
    set((state) => ({
      version: version ?? state.version,
      startupMode: startupMode ?? state.startupMode,
      projectName: projectName ?? state.projectName,
      projectId: projectId ?? state.projectId,
    })),
  startHealthCheck: () => {
    let timerId: ReturnType<typeof setTimeout> | null = null

    const schedule = (connected: boolean) => {
      if (timerId !== null) clearTimeout(timerId)
      timerId = setTimeout(runCheck, connected ? POLL_CONNECTED_MS : POLL_DISCONNECTED_MS)
    }

    const runCheck = async () => {
      try {
        const info = await fetchServerInfo()
        set({
          connected: true,
          version: info.version ?? '',
          startupMode: info.startup_mode || 'unknown',
          projectName: info.project_name || '',
          projectId: info.project_id || '',
        })
        schedule(true)
      } catch {
        set({ connected: false })
        schedule(false)
      }
    }

    // Immediate disconnect notification from api.ts network failures.
    // Re-schedule at fast pace so reconnection is detected quickly.
    registerNetworkErrorHandler(() => {
      if (get().connected) {
        set({ connected: false })
      }
      schedule(false)
    })

    // First check immediately, then schedule adaptively.
    void runCheck()

    return () => {
      if (timerId !== null) clearTimeout(timerId)
    }
  },
}))
