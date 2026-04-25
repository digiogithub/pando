import { create } from 'zustand'
import api from '@/services/api'
import type {
  ContainerCapabilitiesResponse,
  ContainerConfig,
  ContainerEvent,
  ContainerSessionInfo,
  RuntimeCapability,
} from '@/types'
import { useToastStore } from './toastStore'

const DEFAULT_CONFIG: ContainerConfig = {
  runtime: 'host',
  image: '',
  pull_policy: 'if-not-present',
  socket: '',
  work_dir: '',
  network: 'none',
  read_only: true,
  user: '',
  cpu_limit: '',
  mem_limit: '',
  pids_limit: 512,
  no_new_privileges: true,
  allow_env: [],
  allow_mounts: [],
  extra_env: [],
  extra_mounts: [],
}

interface ContainerStore {
  config: ContainerConfig
  original: ContainerConfig
  capabilities: RuntimeCapability[]
  currentRuntime: string
  sessions: ContainerSessionInfo[]
  events: ContainerEvent[]
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchAll: () => Promise<void>
  updateField: <K extends keyof ContainerConfig>(key: K, value: ContainerConfig[K]) => void
  saveConfig: () => Promise<void>
  resetConfig: () => void
  stopSession: (sessionId: string) => Promise<void>
  refreshObservability: () => Promise<void>
}

export const useContainerStore = create<ContainerStore>((set, get) => ({
  config: { ...DEFAULT_CONFIG },
  original: { ...DEFAULT_CONFIG },
  capabilities: [],
  currentRuntime: 'host',
  sessions: [],
  events: [],
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchAll: async () => {
    set({ loading: true, error: null })
    try {
      const [configData, capabilitiesData, sessionsData, eventsData] = await Promise.all([
        api.get<ContainerConfig>('/api/v1/container/config'),
        api.get<ContainerCapabilitiesResponse>('/api/v1/container/capabilities'),
        api.get<{ sessions: ContainerSessionInfo[] }>('/api/v1/container/sessions'),
        api.get<{ events: ContainerEvent[] }>('/api/v1/container/events?limit=100'),
      ])

      const merged = { ...DEFAULT_CONFIG, ...configData }
      set({
        config: merged,
        original: { ...merged },
        capabilities: capabilitiesData.runtimes ?? [],
        currentRuntime: capabilitiesData.current || merged.runtime || 'host',
        sessions: sessionsData.sessions ?? [],
        events: eventsData.events ?? [],
        dirty: false,
      })
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to load container settings'
      set({ error: message })
    } finally {
      set({ loading: false })
    }
  },

  updateField: (key, value) =>
    set((state) => {
      const config = { ...state.config, [key]: value }
      return {
        config,
        dirty: JSON.stringify(config) !== JSON.stringify(state.original),
      }
    }),

  saveConfig: async () => {
    set({ saving: true, error: null })
    try {
      const updated = await api.put<ContainerConfig>('/api/v1/container/config', get().config)
      set((state) => ({
        config: { ...DEFAULT_CONFIG, ...updated },
        original: { ...DEFAULT_CONFIG, ...updated },
        currentRuntime: updated.runtime || state.currentRuntime || 'host',
        dirty: false,
      }))
      useToastStore.getState().addToast('Container runtime settings saved', 'success')
      await get().refreshObservability()
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to save container settings'
      set({ error: message })
      useToastStore.getState().addToast(message, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetConfig: () =>
    set((state) => ({
      config: { ...state.original },
      dirty: false,
    })),

  stopSession: async (sessionId: string) => {
    try {
      await api.post(`/api/v1/container/sessions/${encodeURIComponent(sessionId)}/stop`, {})
      useToastStore.getState().addToast(`Container session "${sessionId}" stopped`, 'success')
      await get().refreshObservability()
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to stop container session'
      set({ error: message })
      useToastStore.getState().addToast(message, 'error')
    }
  },

  refreshObservability: async () => {
    try {
      const [capabilitiesData, sessionsData, eventsData] = await Promise.all([
        api.get<ContainerCapabilitiesResponse>('/api/v1/container/capabilities'),
        api.get<{ sessions: ContainerSessionInfo[] }>('/api/v1/container/sessions'),
        api.get<{ events: ContainerEvent[] }>('/api/v1/container/events?limit=100'),
      ])
      set({
        capabilities: capabilitiesData.runtimes ?? [],
        currentRuntime: capabilitiesData.current || get().config.runtime || 'host',
        sessions: sessionsData.sessions ?? [],
        events: eventsData.events ?? [],
      })
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Failed to refresh container activity'
      set({ error: message })
    }
  },
}))
