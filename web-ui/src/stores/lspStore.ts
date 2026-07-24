import { create } from 'zustand'
import api from '@/services/api'
import type { LSPActivation, LSPConfig, LSPServerStatus } from '@/types'
import { useToastStore } from './toastStore'

const DEFAULT_ACTIVATION: LSPActivation = {
  autoActivate: true,
  activateOn: 'edits',
  autoInstall: true,
  runner: 'auto',
  startupTimeout: '20s',
  installTimeout: '2m0s',
}

interface LSPStore {
  configs: LSPConfig[]
  /** The whole catalogue (presets + user servers) with availability. */
  catalog: LSPServerStatus[]
  activation: LSPActivation
  loading: boolean
  saving: boolean
  error: string | null
  fetchLSP: () => Promise<void>
  fetchCatalog: () => Promise<void>
  saveLSP: (config: LSPConfig) => Promise<void>
  deleteLSP: (language: string) => Promise<void>
  saveActivation: (activation: LSPActivation) => Promise<void>
}

export const useLSPStore = create<LSPStore>((set, get) => ({
  configs: [],
  catalog: [],
  activation: DEFAULT_ACTIVATION,
  loading: false,
  saving: false,
  error: null,

  fetchLSP: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ lsp: LSPConfig[]; activation?: LSPActivation }>('/api/v1/config/lsp')
      set({ configs: data.lsp ?? [], activation: data.activation ?? get().activation })
      await get().fetchCatalog()
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to load LSP configs' })
    } finally {
      set({ loading: false })
    }
  },

  fetchCatalog: async () => {
    try {
      const data = await api.get<{ servers: LSPServerStatus[]; activation?: LSPActivation }>(
        '/api/v1/config/lsp/catalog',
      )
      set({ catalog: data.servers ?? [], activation: data.activation ?? get().activation })
    } catch (e) {
      // The catalogue is informational: a failure must not hide the configured
      // servers, so it only records the error.
      set({ error: e instanceof Error ? e.message : 'Failed to load the LSP catalogue' })
    }
  },

  saveLSP: async (config: LSPConfig) => {
    set({ saving: true, error: null })
    try {
      const data = await api.put<{ lsp: LSPConfig[] }>('/api/v1/config/lsp', config)
      set({ configs: data.lsp ?? get().configs })
      useToastStore.getState().addToast(`LSP "${config.language}" saved`, 'success')
      await get().fetchCatalog()
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  deleteLSP: async (language: string) => {
    set({ saving: true, error: null })
    try {
      await api.delete(`/api/v1/config/lsp/${encodeURIComponent(language)}`)
      set((s) => ({ configs: s.configs.filter((c) => c.language !== language) }))
      useToastStore.getState().addToast(`LSP "${language}" deleted`, 'success')
      await get().fetchCatalog()
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Delete failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  saveActivation: async (activation: LSPActivation) => {
    const previous = get().activation
    set({ saving: true, error: null, activation })
    try {
      const data = await api.put<LSPActivation>('/api/v1/config/lsp/activation', activation)
      set({ activation: data ?? activation })
      useToastStore.getState().addToast('LSP activation settings saved', 'success')
      await get().fetchCatalog()
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg, activation: previous })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },
}))
