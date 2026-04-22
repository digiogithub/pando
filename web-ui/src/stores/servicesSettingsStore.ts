import { create } from 'zustand'
import api from '@/services/api'
import type { ServicesConfig, MesnadaConfig, RemembrancesConfig, SnapshotsConfig, APIServerConfig } from '@/types'
import { useToastStore } from './toastStore'

const DEFAULT_MESNADA: MesnadaConfig = {
  enabled: false,
  server: { host: 'localhost', port: 9090 },
  orchestrator: {
    storePath: '',
    logDir: '',
    maxParallel: 4,
    defaultEngine: 'pando',
    defaultModel: '',
    defaultMcpConfig: '',
    personaPath: '',
  },
  acp: {
    enabled: false,
    defaultAgent: '',
    autoPermission: false,
    server: {
      enabled: false,
      transports: [],
      host: 'localhost',
      port: 9091,
      maxSessions: 10,
      sessionTimeout: '1h',
      requireAuth: false,
    },
  },
  tui: { enabled: true, webui: false },
}

const DEFAULT_REMEMBRANCES: RemembrancesConfig = {
  enabled: false,
  kb_path: '',
  kb_watch: true,
  kb_auto_import: true,
  document_embedding_provider: '',
  document_embedding_model: '',
  document_embedding_base_url: '',
  document_embedding_api_key: '',
  code_embedding_provider: '',
  code_embedding_model: '',
  code_embedding_base_url: '',
  code_embedding_api_key: '',
  use_same_model: false,
  chunk_size: 512,
  chunk_overlap: 64,
  index_workers: 2,
  context_enrichment_enabled: false,
  context_enrichment_kb_results: 3,
  context_enrichment_code_results: 5,
  context_enrichment_code_project: '',
  context_enrichment_events_results: 3,
  context_enrichment_events_subject: '',
  context_enrichment_events_last_days: 30,
}

const DEFAULT_SNAPSHOTS: SnapshotsConfig = {
  enabled: false,
  maxSnapshots: 50,
  maxFileSize: '10MB',
  excludePatterns: [],
  autoCleanupDays: 30,
}

const DEFAULT_API_SERVER: APIServerConfig = {
  enabled: true,
  host: 'localhost',
  port: 9999,
  requireAuth: false,
}

const DEFAULTS: ServicesConfig = {
  mesnada: DEFAULT_MESNADA,
  remembrances: DEFAULT_REMEMBRANCES,
  snapshots: DEFAULT_SNAPSHOTS,
  server: DEFAULT_API_SERVER,
}

interface ServicesSettingsStore {
  config: ServicesConfig
  original: ServicesConfig
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fetchServices: () => Promise<void>
  updateMesnada: <K extends keyof MesnadaConfig>(key: K, value: MesnadaConfig[K]) => void
  updateRemembrances: <K extends keyof RemembrancesConfig>(key: K, value: RemembrancesConfig[K]) => void
  updateSnapshots: <K extends keyof SnapshotsConfig>(key: K, value: SnapshotsConfig[K]) => void
  updateServer: <K extends keyof APIServerConfig>(key: K, value: APIServerConfig[K]) => void
  saveServices: () => Promise<void>
  resetServices: () => void
}

export const useServicesSettingsStore = create<ServicesSettingsStore>((set, get) => ({
  config: { ...DEFAULTS },
  original: { ...DEFAULTS },
  dirty: false,
  loading: false,
  saving: false,
  error: null,

  fetchServices: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<ServicesConfig>('/api/v1/config/services')
      const merged: ServicesConfig = {
        mesnada: { ...DEFAULT_MESNADA, ...data.mesnada, orchestrator: { ...DEFAULT_MESNADA.orchestrator, ...data.mesnada?.orchestrator }, acp: { ...DEFAULT_MESNADA.acp, ...data.mesnada?.acp, server: { ...DEFAULT_MESNADA.acp.server, ...data.mesnada?.acp?.server } }, tui: { ...DEFAULT_MESNADA.tui, ...data.mesnada?.tui } },
        remembrances: { ...DEFAULT_REMEMBRANCES, ...data.remembrances },
        snapshots: { ...DEFAULT_SNAPSHOTS, ...data.snapshots },
        server: { ...DEFAULT_API_SERVER, ...data.server },
      }
      set({ config: merged, original: merged, dirty: false })
    } catch {
      set({ config: { ...DEFAULTS }, original: { ...DEFAULTS } })
    } finally {
      set({ loading: false })
    }
  },

  updateMesnada: (key, value) =>
    set((s) => {
      const config = { ...s.config, mesnada: { ...s.config.mesnada, [key]: value } }
      return { config, dirty: JSON.stringify(config) !== JSON.stringify(s.original) }
    }),

  updateRemembrances: (key, value) =>
    set((s) => {
      const config = { ...s.config, remembrances: { ...s.config.remembrances, [key]: value } }
      return { config, dirty: JSON.stringify(config) !== JSON.stringify(s.original) }
    }),

  updateSnapshots: (key, value) =>
    set((s) => {
      const config = { ...s.config, snapshots: { ...s.config.snapshots, [key]: value } }
      return { config, dirty: JSON.stringify(config) !== JSON.stringify(s.original) }
    }),

  updateServer: (key, value) =>
    set((s) => {
      const config = { ...s.config, server: { ...s.config.server, [key]: value } }
      return { config, dirty: JSON.stringify(config) !== JSON.stringify(s.original) }
    }),

  saveServices: async () => {
    set({ saving: true, error: null })
    try {
      await api.put('/api/v1/config/services', get().config)
      set((s) => ({ original: JSON.parse(JSON.stringify(s.config)), dirty: false }))
      useToastStore.getState().addToast('Services settings saved', 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  resetServices: () =>
    set((s) => ({ config: JSON.parse(JSON.stringify(s.original)), dirty: false })),
}))
