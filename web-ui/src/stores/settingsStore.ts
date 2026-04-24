import { create } from 'zustand'
import api from '@/services/api'
import type { SettingsConfig, ProviderConfigItem, AgentConfigItem, ToolsConfig, BashConfig } from '@/types'
import { useToastStore } from './toastStore'

const DEFAULTS: SettingsConfig = {
  home_directory: '',
  default_model: 'claude-sonnet-4-6',
  default_provider: 'anthropic',
  language: 'en',
  theme: 'pando-light',
  auto_save: true,
  markdown_preview: true,
  custom_instructions: '',
  llm_cache_enabled: true,
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

// ---- Providers Store ----

interface ProvidersStore {
  providers: ProviderConfigItem[]
  original: ProviderConfigItem[]
  // Track which providers have a new (user-typed) API key. Never stored in state — only as a local draft.
  // We keep a dirty flag per provider name.
  dirtyKeys: Record<string, string> // providerName -> new key typed by user (never logged)
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchProviders: () => Promise<void>
  updateProvider: (name: string, patch: Partial<ProviderConfigItem>) => void
  updateProviderKey: (name: string, newKey: string) => void
  saveProviders: () => Promise<void>
  resetProviders: () => void
}

export const useProvidersStore = create<ProvidersStore>((set, get) => ({
  providers: [],
  original: [],
  dirtyKeys: {},
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchProviders: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ providers: ProviderConfigItem[] }>('/api/v1/config/providers')
      const providers = data.providers ?? []
      set({ providers, original: providers.map((p) => ({ ...p })), dirty: false, dirtyKeys: {} })
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load providers'
      set({ error: msg })
    } finally {
      set({ loading: false })
    }
  },

  updateProvider: (name, patch) =>
    set((s) => {
      const providers = s.providers.map((p) =>
        p.name === name ? { ...p, ...patch } : p
      )
      const dirty =
        JSON.stringify(providers) !== JSON.stringify(s.original) ||
        Object.keys(s.dirtyKeys).length > 0
      return { providers, dirty }
    }),

  updateProviderKey: (name, newKey) =>
    set((s) => {
      const dirtyKeys = { ...s.dirtyKeys, [name]: newKey }
      return { dirtyKeys, dirty: true }
    }),

  saveProviders: async () => {
    set({ saving: true, error: null })
    const { providers, dirtyKeys } = get()
    // Merge dirty keys into the payload — never log them
    const payload = providers.map((p) => ({
      ...p,
      apiKey: dirtyKeys[p.name] !== undefined ? dirtyKeys[p.name] : p.apiKey,
    }))
    try {
      const data = await api.put<{ providers: ProviderConfigItem[] }>(
        '/api/v1/config/providers',
        { providers: payload }
      )
      const updated = data.providers ?? []
      set({ providers: updated, original: updated.map((p) => ({ ...p })), dirty: false, dirtyKeys: {} })
      useToastStore.getState().addToast('Providers saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetProviders: () =>
    set((s) => ({ providers: s.original.map((p) => ({ ...p })), dirtyKeys: {}, dirty: false })),
}))

// ---- Agents Store ----

interface AgentsStore {
  agents: AgentConfigItem[]
  original: AgentConfigItem[]
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchAgents: () => Promise<void>
  updateAgent: (name: string, patch: Partial<AgentConfigItem>) => void
  saveAgents: () => Promise<void>
  resetAgents: () => void
}

export const useAgentsStore = create<AgentsStore>((set, get) => ({
  agents: [],
  original: [],
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchAgents: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ agents: AgentConfigItem[] }>('/api/v1/config/agents')
      const agents = data.agents ?? []
      set({ agents, original: agents.map((a) => ({ ...a })), dirty: false })
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load agents'
      set({ error: msg })
    } finally {
      set({ loading: false })
    }
  },

  updateAgent: (name, patch) =>
    set((s) => {
      const exists = s.agents.some((a) => a.name === name)
      const agents = exists
        ? s.agents.map((a) => (a.name === name ? { ...a, ...patch } : a))
        : [...s.agents, { name, model: '', maxTokens: 0, reasoningEffort: '', autoCompact: false, autoCompactThreshold: 0, ...patch }]
      return { agents, dirty: JSON.stringify(agents) !== JSON.stringify(s.original) }
    }),

  saveAgents: async () => {
    set({ saving: true, error: null })
    try {
      const data = await api.put<{ agents: AgentConfigItem[] }>(
        '/api/v1/config/agents',
        { agents: get().agents }
      )
      const updated = data.agents ?? []
      set({ agents: updated, original: updated.map((a) => ({ ...a })), dirty: false })
      useToastStore.getState().addToast('Agents saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetAgents: () =>
    set((s) => ({ agents: s.original.map((a) => ({ ...a })), dirty: false })),
}))

// ---- Tools Store ----

const TOOLS_DEFAULTS: ToolsConfig = {
  fetchEnabled: false,
  fetchMaxSizeMB: 10,
  googleSearchEnabled: false,
  googleApiKey: '',
  googleSearchEngineId: '',
  braveSearchEnabled: false,
  braveApiKey: '',
  perplexitySearchEnabled: false,
  perplexityApiKey: '',
  exaSearchEnabled: false,
  exaApiKey: '',
  context7Enabled: false,
  browserEnabled: false,
  browserHeadless: true,
  browserTimeout: 30,
  browserUserDataDir: '',
  browserMaxSessions: 3,
}

interface ToolsStore {
  config: ToolsConfig
  original: ToolsConfig
  // Dirty key drafts (user-typed API keys before save)
  dirtyKeys: Partial<Record<keyof ToolsConfig, string>>
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchTools: () => Promise<void>
  updateField: <K extends keyof ToolsConfig>(key: K, value: ToolsConfig[K]) => void
  updateApiKey: (field: keyof ToolsConfig, value: string) => void
  saveTools: () => Promise<void>
  resetTools: () => void
}

export const useToolsStore = create<ToolsStore>((set, get) => ({
  config: { ...TOOLS_DEFAULTS },
  original: { ...TOOLS_DEFAULTS },
  dirtyKeys: {},
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchTools: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<ToolsConfig>('/api/v1/config/tools')
      const merged = { ...TOOLS_DEFAULTS, ...data }
      set({ config: merged, original: { ...merged }, dirty: false, dirtyKeys: {} })
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load tools config'
      set({ error: msg })
    } finally {
      set({ loading: false })
    }
  },

  updateField: (key, value) =>
    set((s) => {
      const config = { ...s.config, [key]: value }
      const dirty =
        JSON.stringify(config) !== JSON.stringify(s.original) ||
        Object.keys(s.dirtyKeys).length > 0
      return { config, dirty }
    }),

  updateApiKey: (field, value) =>
    set((s) => ({
      dirtyKeys: { ...s.dirtyKeys, [field]: value },
      dirty: true,
    })),

  saveTools: async () => {
    set({ saving: true, error: null })
    const { config, dirtyKeys } = get()
    // Merge user-typed API keys into payload
    const payload = { ...config, ...dirtyKeys }
    try {
      const data = await api.put<ToolsConfig>('/api/v1/config/tools', payload)
      const updated = { ...TOOLS_DEFAULTS, ...data }
      set({ config: updated, original: { ...updated }, dirty: false, dirtyKeys: {} })
      useToastStore.getState().addToast('Tools saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetTools: () =>
    set((s) => ({ config: { ...s.original }, dirtyKeys: {}, dirty: false })),
}))

// ---- Bash Store ----

const BASH_DEFAULTS: BashConfig = {
  bannedCommands: [],
  allowedCommands: [],
}

interface BashStore {
  config: BashConfig
  original: BashConfig
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchBash: () => Promise<void>
  updateField: <K extends keyof BashConfig>(key: K, value: BashConfig[K]) => void
  saveBash: () => Promise<void>
  resetBash: () => void
}

export const useBashStore = create<BashStore>((set, get) => ({
  config: { ...BASH_DEFAULTS },
  original: { ...BASH_DEFAULTS },
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchBash: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<BashConfig>('/api/v1/config/bash')
      const merged = { ...BASH_DEFAULTS, ...data }
      set({ config: merged, original: { ...merged }, dirty: false })
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load bash config'
      set({ error: msg })
    } finally {
      set({ loading: false })
    }
  },

  updateField: (key, value) =>
    set((s) => {
      const config = { ...s.config, [key]: value }
      return { config, dirty: JSON.stringify(config) !== JSON.stringify(s.original) }
    }),

  saveBash: async () => {
    set({ saving: true, error: null })
    try {
      const data = await api.put<BashConfig>('/api/v1/config/bash', get().config)
      const updated = { ...BASH_DEFAULTS, ...data }
      set({ config: updated, original: { ...updated }, dirty: false })
      useToastStore.getState().addToast('Bash settings saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetBash: () =>
    set((s) => ({ config: { ...s.original }, dirty: false })),
}))
