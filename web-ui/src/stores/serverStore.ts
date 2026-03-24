import { create } from 'zustand'
import { checkHealth } from '@/services/auth'

interface ServerStore {
  connected: boolean
  version: string
  setConnected: (v: boolean) => void
  setVersion: (v: string) => void
  startHealthCheck: () => () => void
}

export const useServerStore = create<ServerStore>((set) => ({
  connected: false,
  version: '',
  setConnected: (connected) => set({ connected }),
  setVersion: (version) => set({ version }),
  startHealthCheck: () => {
    const check = async () => {
      const ok = await checkHealth()
      set({ connected: ok })
    }
    check()
    const interval = setInterval(check, 30_000)
    return () => clearInterval(interval)
  },
}))
