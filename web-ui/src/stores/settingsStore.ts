import { create } from 'zustand'
import api from '@/services/api'
import type { SettingsConfig, ProviderConfigItem, AgentConfigItem } from '@/types'
import { useToastStore } from './toastStore'

const DEFAULTS: SettingsConfig = {
  home_directory: '',
  default_model: 'claude-sonnet-4-6',
  default_provider: 'anthropic',
  language: 'en',
  theme: 'light',
  auto_save: true,
  markdown_preview: true,
  custom_instructions: '',
}

interface SettingsStore {
  config: SettingsConfig
  original: SettingsConfig
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchSettings: () => Promise<void>
  updateField: <K extends keyof SettingsConfig>(key: K, value: SettingsConfig[K]) => void
  saveSettings: () => Promise<void>
  resetSettings: () => void
}

export const useSettingsStore = create<SettingsStore>((set, get) => ({
  config: { ...DEFAULTS },
  original: { ...DEFAULTS },
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchSettings: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<SettingsConfig>('/api/v1/settings')
      const merged = { ...DEFAULTS, ...data }
      set({ config: merged, original: merged, dirty: false })
    } catch {
      // Backend may not have this endpoint yet — use defaults silently
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

  saveSettings: async () => {
    set({ saving: true, error: null })
    try {
      await api.put('/api/v1/settings', get().config)
      set((s) => ({ original: { ...s.config }, dirty: false }))
      useToastStore.getState().addToast('Settings saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetSettings: () =>
    set((s) => ({ config: { ...s.original }, dirty: false })),
}))
