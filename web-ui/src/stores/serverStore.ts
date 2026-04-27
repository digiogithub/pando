import { create } from 'zustand'
import { checkHealth } from '@/services/auth'
import { registerNetworkErrorHandler } from '@/services/api'

// Polling interval when connected (relaxed) vs disconnected (fast reconnect detection).
const POLL_CONNECTED_MS = 30_000
const POLL_DISCONNECTED_MS = 5_000

interface ServerStore {
  connected: boolean
  version: string
  setConnected: (v: boolean) => void
  setVersion: (v: string) => void
  startHealthCheck: () => () => void
}

export const useServerStore = create<ServerStore>((set, get) => ({
  connected: false,
  version: '',
  setConnected: (connected) => set({ connected }),
  setVersion: (version) => set({ version }),
  startHealthCheck: () => {
    let timerId: ReturnType<typeof setTimeout> | null = null

    const schedule = (connected: boolean) => {
      if (timerId !== null) clearTimeout(timerId)
      timerId = setTimeout(runCheck, connected ? POLL_CONNECTED_MS : POLL_DISCONNECTED_MS)
    }

    const runCheck = async () => {
      const ok = await checkHealth()
      set({ connected: ok })
      schedule(ok)
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
