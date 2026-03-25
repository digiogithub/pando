import { create } from 'zustand'
import api from '@/services/api'
import type { LSPConfig } from '@/types'
import { useToastStore } from './toastStore'

interface LSPStore {
  configs: LSPConfig[]
  loading: boolean
  saving: boolean
  error: string | null
  fetchLSP: () => Promise<void>
  saveLSP: (config: LSPConfig) => Promise<void>
  deleteLSP: (language: string) => Promise<void>
}

export const useLSPStore = create<LSPStore>((set, get) => ({
  configs: [],
  loading: false,
  saving: false,
  error: null,

  fetchLSP: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ lsp: LSPConfig[] }>('/api/v1/config/lsp')
      set({ configs: data.lsp ?? [] })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to load LSP configs' })
    } finally {
      set({ loading: false })
    }
  },

  saveLSP: async (config: LSPConfig) => {
    set({ saving: true, error: null })
    try {
      const data = await api.put<{ lsp: LSPConfig[] }>('/api/v1/config/lsp', config)
      set({ configs: data.lsp ?? get().configs })
      useToastStore.getState().addToast(`LSP "${config.language}" saved`, 'success')
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
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Delete failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },
}))
