import { create } from 'zustand'
import { api } from '@/services/api'

interface ConfigInitStatus {
  hasLocalConfig: boolean
  hasPandoDir: boolean
  shouldGenerate: boolean
}

interface ConfigInitState {
  status: ConfigInitStatus | null
  loading: boolean
  generating: boolean
  dismissed: boolean
  fetchStatus: () => Promise<void>
  generateConfig: () => Promise<boolean>
  dismiss: () => void
}

export const useConfigInitStore = create<ConfigInitState>((set, get) => ({
  status: null,
  loading: false,
  generating: false,
  dismissed: false,

  fetchStatus: async () => {
    if (get().loading) return
    set({ loading: true })
    try {
      const status = await api.get<ConfigInitStatus>('/api/v1/config/init-status')
      set({ status, loading: false })
    } catch {
      set({ loading: false })
    }
  },

  generateConfig: async () => {
    set({ generating: true })
    try {
      await api.post('/api/v1/config/generate', {})
      // Refresh status after generation
      const status = await api.get<ConfigInitStatus>('/api/v1/config/init-status')
      set({ status, generating: false })
      return true
    } catch {
      set({ generating: false })
      return false
    }
  },

  dismiss: () => set({ dismissed: true }),
}))
