import { create } from 'zustand'
import api from '@/services/api'
import type { ExtensionsConfig, EvaluatorSettingsConfig } from '@/types'
import { useToastStore } from './toastStore'

// ---- Default values ----

const EXTENSIONS_DEFAULTS: ExtensionsConfig = {
  skills: {
    enabled: true,
    paths: [],
  },
  skillsCatalog: {
    enabled: false,
    baseUrl: 'https://skills.sh',
    autoUpdate: false,
    defaultScope: 'session',
  },
  lua: {
    enabled: false,
    script_path: '',
    timeout: '30s',
    strict_mode: false,
    hot_reload: false,
    log_filtered_data: false,
  },
}

const EVALUATOR_DEFAULTS: EvaluatorSettingsConfig = {
  enabled: false,
  model: '',
  provider: '',
  alphaWeight: 0.8,
  betaWeight: 0.2,
  explorationC: 1.41,
  minSessionsForUCB: 5,
  correctionsPatterns: [],
  maxTokensBaseline: 50,
  maxSkills: 100,
  judgePromptTemplate: '',
  async: true,
}

// ---- Extensions slice ----

interface ExtensionsSlice {
  extensions: ExtensionsConfig
  extensionsOriginal: ExtensionsConfig
  extensionsDirty: boolean
  extensionsLoading: boolean
  extensionsSaving: boolean
  extensionsError: string | null
  fetchExtensions: () => Promise<void>
  updateExtensions: (patch: Partial<ExtensionsConfig>) => void
  saveExtensions: () => Promise<void>
  resetExtensions: () => void
}

// ---- Evaluator slice ----

interface EvaluatorSlice {
  evaluator: EvaluatorSettingsConfig
  evaluatorOriginal: EvaluatorSettingsConfig
  evaluatorDirty: boolean
  evaluatorLoading: boolean
  evaluatorSaving: boolean
  evaluatorError: string | null
  fetchEvaluator: () => Promise<void>
  updateEvaluator: (patch: Partial<EvaluatorSettingsConfig>) => void
  saveEvaluator: () => Promise<void>
  resetEvaluator: () => void
}

type ExtensionsStore = ExtensionsSlice & EvaluatorSlice

export const useExtensionsStore = create<ExtensionsStore>((set, get) => ({
  // ---- Extensions ----
  extensions: { ...EXTENSIONS_DEFAULTS },
  extensionsOriginal: { ...EXTENSIONS_DEFAULTS },
  extensionsDirty: false,
  extensionsLoading: false,
  extensionsSaving: false,
  extensionsError: null,

  fetchExtensions: async () => {
    set({ extensionsLoading: true, extensionsError: null })
    try {
      const data = await api.get<ExtensionsConfig>('/api/v1/config/extensions')
      const merged: ExtensionsConfig = {
        skills: { ...EXTENSIONS_DEFAULTS.skills, ...data.skills },
        skillsCatalog: { ...EXTENSIONS_DEFAULTS.skillsCatalog, ...data.skillsCatalog },
        lua: { ...EXTENSIONS_DEFAULTS.lua, ...data.lua },
      }
      set({ extensions: merged, extensionsOriginal: merged, extensionsDirty: false })
    } catch {
      set({
        extensions: { ...EXTENSIONS_DEFAULTS },
        extensionsOriginal: { ...EXTENSIONS_DEFAULTS },
      })
    } finally {
      set({ extensionsLoading: false })
    }
  },

  updateExtensions: (patch) =>
    set((s) => {
      const extensions = { ...s.extensions, ...patch }
      return {
        extensions,
        extensionsDirty: JSON.stringify(extensions) !== JSON.stringify(s.extensionsOriginal),
      }
    }),

  saveExtensions: async () => {
    set({ extensionsSaving: true, extensionsError: null })
    try {
      await api.put('/api/v1/config/extensions', get().extensions)
      set((s) => ({
        extensionsOriginal: { ...s.extensions },
        extensionsDirty: false,
      }))
      useToastStore.getState().addToast('Extensions settings saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ extensionsError: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ extensionsSaving: false })
    }
  },

  resetExtensions: () =>
    set((s) => ({ extensions: { ...s.extensionsOriginal }, extensionsDirty: false })),

  // ---- Evaluator ----
  evaluator: { ...EVALUATOR_DEFAULTS },
  evaluatorOriginal: { ...EVALUATOR_DEFAULTS },
  evaluatorDirty: false,
  evaluatorLoading: false,
  evaluatorSaving: false,
  evaluatorError: null,

  fetchEvaluator: async () => {
    set({ evaluatorLoading: true, evaluatorError: null })
    try {
      const data = await api.get<EvaluatorSettingsConfig>('/api/v1/config/evaluator')
      const merged = { ...EVALUATOR_DEFAULTS, ...data }
      set({ evaluator: merged, evaluatorOriginal: merged, evaluatorDirty: false })
    } catch {
      set({
        evaluator: { ...EVALUATOR_DEFAULTS },
        evaluatorOriginal: { ...EVALUATOR_DEFAULTS },
      })
    } finally {
      set({ evaluatorLoading: false })
    }
  },

  updateEvaluator: (patch) =>
    set((s) => {
      const evaluator = { ...s.evaluator, ...patch }
      return {
        evaluator,
        evaluatorDirty: JSON.stringify(evaluator) !== JSON.stringify(s.evaluatorOriginal),
      }
    }),

  saveEvaluator: async () => {
    set({ evaluatorSaving: true, evaluatorError: null })
    try {
      await api.put('/api/v1/config/evaluator', get().evaluator)
      set((s) => ({
        evaluatorOriginal: { ...s.evaluator },
        evaluatorDirty: false,
      }))
      useToastStore.getState().addToast('Evaluator settings saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ evaluatorError: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ evaluatorSaving: false })
    }
  },

  resetEvaluator: () =>
    set((s) => ({ evaluator: { ...s.evaluatorOriginal }, evaluatorDirty: false })),
}))
