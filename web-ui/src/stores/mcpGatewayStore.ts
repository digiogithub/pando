import { create } from 'zustand'
import api from '@/services/api'
import type { MCPGatewayConfig } from '@/types'
import { useToastStore } from './toastStore'

const DEFAULTS: MCPGatewayConfig = {
  enabled: false,
  favorite_threshold: 3,
  max_favorites: 10,
  favorite_window_days: 7,
  decay_days: 30,
}

interface MCPGatewayStore {
  config: MCPGatewayConfig
  original: MCPGatewayConfig
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchGateway: () => Promise<void>
  updateField: <K extends keyof MCPGatewayConfig>(key: K, value: MCPGatewayConfig[K]) => void
  saveGateway: () => Promise<void>
  resetGateway: () => void
}

export const useMCPGatewayStore = create<MCPGatewayStore>((set, get) => ({
  config: { ...DEFAULTS },
  original: { ...DEFAULTS },
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchGateway: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<MCPGatewayConfig>('/api/v1/config/mcp-gateway')
      const merged = { ...DEFAULTS, ...data }
      set({ config: merged, original: merged, dirty: false })
    } catch {
      set({ config: { ...DEFAULTS }, original: { ...DEFAULTS } })
    } finally {
      set({ loading: false })
    }
  },

  updateField: (key, value) =>
    set((s) => {
      const config = { ...s.config, [key]: value }
      return { config, dirty: JSON.stringify(config) !== JSON.stringify(s.original) }
    }),

  saveGateway: async () => {
    set({ saving: true, error: null })
    try {
      const data = await api.put<MCPGatewayConfig>('/api/v1/config/mcp-gateway', get().config)
      const merged = { ...DEFAULTS, ...data }
      set({ config: merged, original: merged, dirty: false })
      useToastStore.getState().addToast('MCP Gateway settings saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetGateway: () =>
    set((s) => ({ config: { ...s.original }, dirty: false })),
}))
