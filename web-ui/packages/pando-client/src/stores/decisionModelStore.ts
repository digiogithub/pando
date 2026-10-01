import { create } from 'zustand'
import api from '../services/api'
import { useToastStore } from './toastStore'
import {
  parseApiError,
  type DecisionProviderKind,
  type DecisionRouterDraft,
  type DecisionRouterInfo,
  type FieldError,
  type HealthReportDTO,
  type RouterModelsResponse,
  type RouterPullJob,
  type RouterTestResponse,
} from './modelAutoModeStore'

export type { DecisionProviderKind, DecisionRouterDraft, DecisionRouterInfo, HealthReportDTO }

/** The editable block, same shape as the PUT body (minus clearApiKey). */
export interface DecisionModelDraft {
  router: DecisionRouterDraft
  timeoutMs: number
}

/** GET/PUT /api/v1/config/decision-model response. */
export interface DecisionModelResponse {
  router: Omit<DecisionRouterDraft, 'apiKey'> & DecisionRouterInfo
  timeoutMs: number
  warnings?: string[]
}

export interface DecisionHealthResponse {
  ok: boolean
  report?: HealthReportDTO
  problems?: string[]
  error?: string
}

const BASE = '/api/v1/decision-model/router'

export const EMPTY_DECISION_DRAFT: DecisionModelDraft = {
  router: { provider: 'ollama', baseURL: '', model: '', keepAlive: '', headers: {}, apiKey: '' },
  timeoutMs: 0,
}

/** A provider that is not the local Ollama sends prompts off the machine. */
export function isHostedDecisionProvider(provider: string, effectiveBaseURL?: string): boolean {
  if (provider === 'ollama') return false
  if (effectiveBaseURL) {
    try {
      const h = new URL(effectiveBaseURL).hostname
      if (h === 'localhost' || h === '127.0.0.1' || h === '::1' || h === '[::1]') return false
    } catch {
      // not a URL: treat as hosted
    }
  }
  return true
}

function fromResponse(r: DecisionModelResponse): { draft: DecisionModelDraft; info: DecisionRouterInfo } {
  return {
    draft: {
      router: {
        provider: r.router.provider || 'ollama',
        baseURL: r.router.baseURL ?? '',
        model: r.router.model ?? '',
        keepAlive: r.router.keepAlive ?? '',
        headers: { ...(r.router.headers ?? {}) },
        apiKey: '',
      },
      timeoutMs: r.timeoutMs ?? 0,
    },
    info: {
      effectiveBaseURL: r.router.effectiveBaseURL ?? '',
      apiKeySet: !!r.router.apiKeySet,
      apiKeyMasked: r.router.apiKeyMasked ?? '',
    },
  }
}

/** The router block sent to router/models, router/test and router/pull drafts. */
function routerPayload(d: DecisionRouterDraft) {
  const out: Record<string, unknown> = {
    provider: d.provider,
    baseURL: d.baseURL,
    model: d.model,
    keepAlive: d.keepAlive,
    headers: d.headers,
  }
  if (d.apiKey) out.apiKey = d.apiKey
  return out
}

function isDirty(s: { draft: DecisionModelDraft; original: DecisionModelDraft; clearApiKey: boolean }) {
  return s.clearApiKey || JSON.stringify(s.draft) !== JSON.stringify(s.original)
}

interface DecisionModelStore {
  draft: DecisionModelDraft
  /** The saved block: what the "decision model in use" rows display. */
  original: DecisionModelDraft
  info: DecisionRouterInfo
  clearApiKey: boolean
  dirty: boolean
  loading: boolean
  loaded: boolean
  saving: boolean
  error: string | null
  fieldErrors: FieldError[]
  warnings: string[]

  routerModels: RouterModelsResponse | null
  routerModelsLoading: boolean
  showAllModels: boolean
  pulls: Record<string, RouterPullJob>

  testing: boolean
  testResult: RouterTestResponse | null

  health: HealthReportDTO | null
  healthError: string
  healthLoading: boolean

  fetchConfig: () => Promise<void>
  update: (patch: Partial<Omit<DecisionModelDraft, 'router'>>) => void
  updateRouter: (patch: Partial<DecisionRouterDraft>) => void
  setClearApiKey: (v: boolean) => void
  save: () => Promise<boolean>
  reset: () => void
  setShowAllModels: (v: boolean) => void
  loadRouterModels: (showAll?: boolean) => Promise<void>
  pullModel: (model: string) => Promise<void>
  testConnection: () => Promise<void>
  /** Health of the SAVED router (the server caches it for 60 s). */
  fetchHealth: () => Promise<void>
  errorsFor: (field: string) => string[]
}

export const useDecisionModelStore = create<DecisionModelStore>((set, get) => ({
  draft: EMPTY_DECISION_DRAFT,
  original: EMPTY_DECISION_DRAFT,
  info: { effectiveBaseURL: '', apiKeySet: false, apiKeyMasked: '' },
  clearApiKey: false,
  dirty: false,
  loading: false,
  loaded: false,
  saving: false,
  error: null,
  fieldErrors: [],
  warnings: [],
  routerModels: null,
  routerModelsLoading: false,
  showAllModels: false,
  pulls: {},
  testing: false,
  testResult: null,
  health: null,
  healthError: '',
  healthLoading: false,

  fetchConfig: async () => {
    set({ loading: true, error: null })
    try {
      const r = await api.get<DecisionModelResponse>('/api/v1/config/decision-model')
      const { draft, info } = fromResponse(r)
      set({
        draft,
        original: draft,
        info,
        warnings: r.warnings ?? [],
        clearApiKey: false,
        dirty: false,
        fieldErrors: [],
        loaded: true,
      })
    } catch (e) {
      set({ error: parseApiError(e).error })
    } finally {
      set({ loading: false })
    }
  },

  update: (patch) =>
    set((s) => {
      const draft = { ...s.draft, ...patch }
      return { draft, dirty: isDirty({ draft, original: s.original, clearApiKey: s.clearApiKey }) }
    }),

  updateRouter: (patch) =>
    set((s) => {
      const draft = { ...s.draft, router: { ...s.draft.router, ...patch } }
      // Typing a new key cancels a pending "clear".
      const clearApiKey = patch.apiKey ? false : s.clearApiKey
      return { draft, clearApiKey, dirty: isDirty({ draft, original: s.original, clearApiKey }) }
    }),

  setClearApiKey: (v) =>
    set((s) => {
      const draft = v ? { ...s.draft, router: { ...s.draft.router, apiKey: '' } } : s.draft
      return { draft, clearApiKey: v, dirty: isDirty({ draft, original: s.original, clearApiKey: v }) }
    }),

  save: async () => {
    const { draft, clearApiKey } = get()
    set({ saving: true, error: null, fieldErrors: [] })
    try {
      const body: Record<string, unknown> = { ...draft, router: { ...draft.router } }
      if (clearApiKey) body.clearApiKey = true
      const r = await api.put<DecisionModelResponse>('/api/v1/config/decision-model', body)
      const next = fromResponse(r)
      set({
        draft: next.draft,
        original: next.draft,
        info: next.info,
        warnings: r.warnings ?? [],
        clearApiKey: false,
        dirty: false,
        testResult: null,
      })
      useToastStore.getState().addToast('Decision model settings saved', 'success')
      void get().fetchHealth()
      return true
    } catch (e) {
      const parsed = parseApiError(e)
      set({ error: parsed.error, fieldErrors: parsed.errors ?? [] })
      useToastStore.getState().addToast(parsed.error, 'error')
      return false
    } finally {
      set({ saving: false })
    }
  },

  reset: () =>
    set((s) => ({ draft: s.original, clearApiKey: false, dirty: false, fieldErrors: [], error: null })),

  setShowAllModels: (v) => set({ showAllModels: v }),

  loadRouterModels: async (showAll) => {
    const all = showAll ?? get().showAllModels
    set({ routerModelsLoading: true })
    try {
      const r = await api.post<RouterModelsResponse>(`${BASE}/models`, {
        router: routerPayload(get().draft.router),
        showAll: all,
      })
      set({ routerModels: r })
    } catch (e) {
      set({ routerModels: { models: [], status: 'unsupported', error: parseApiError(e).error } })
    } finally {
      set({ routerModelsLoading: false })
    }
  },

  pullModel: async (model) => {
    const setPull = (job: RouterPullJob) => set((st) => ({ pulls: { ...st.pulls, [model]: job } }))
    const failed = (error: string) => setPull({ id: '', target: model, state: 'error', completed: 0, total: 0, error })
    try {
      let job = await api.post<RouterPullJob>(`${BASE}/pull`, {
        model,
        router: routerPayload(get().draft.router),
      })
      setPull(job)
      while (job.state === 'running') {
        await new Promise((r) => setTimeout(r, 1000))
        job = await api.get<RouterPullJob>(`${BASE}/pull/${job.id}`)
        setPull(job)
      }
      if (job.state === 'done') await get().loadRouterModels()
    } catch (e) {
      failed(parseApiError(e).error)
    }
  },

  testConnection: async () => {
    set({ testing: true, testResult: null })
    try {
      const r = await api.post<RouterTestResponse>(`${BASE}/test`, {
        router: routerPayload(get().draft.router),
      })
      set({ testResult: r })
    } catch (e) {
      set({ testResult: { ok: false, error: parseApiError(e).error } })
    } finally {
      set({ testing: false })
    }
  },

  fetchHealth: async () => {
    if (!get().original.router.model.trim()) {
      set({ health: null, healthError: '' })
      return
    }
    set({ healthLoading: true })
    try {
      const r = await api.get<DecisionHealthResponse>(`${BASE}/health`)
      set({ health: r.report ?? null, healthError: r.report ? '' : (r.error ?? '') })
    } catch (e) {
      set({ health: null, healthError: parseApiError(e).error })
    } finally {
      set({ healthLoading: false })
    }
  },

  errorsFor: (field) => {
    const full = `decisionModel.${field}`
    return get()
      .fieldErrors.filter((e) => e.field === full || e.field === field)
      .map((e) => e.message)
  },
}))
