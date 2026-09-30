import { create } from 'zustand'
import api from '../services/api'
import { useToastStore } from './toastStore'

/** The reserved model id of the "Auto" entry (server: config.AutoModelID). */
export const AUTO_MODEL_ID = 'auto'

export type DecisionProviderKind = 'ollama' | 'typesafe' | 'custom'

export interface DecisionRouterDraft {
  provider: DecisionProviderKind
  baseURL: string
  model: string
  keepAlive: string
  headers: Record<string, string>
  /** Plaintext key typed by the user. Empty keeps the stored key. Never populated from the server. */
  apiKey: string
}

export interface DecisionRouterInfo {
  effectiveBaseURL: string
  apiKeySet: boolean
  apiKeyMasked: string
}

export interface ModelAutoRoute {
  id: string
  description: string
  model: string
  fallbacks: string[]
  disabled: boolean
}

/** The editable block, same shape as the PUT body (minus clearApiKey). */
export interface ModelAutoModeDraft {
  enabled: boolean
  defaultAuto: boolean
  router: DecisionRouterDraft
  threshold: number
  minConfidence: number
  timeoutMs: number
  historyPrompts: number
  routes: ModelAutoRoute[]
}

/** GET/PUT response. */
export interface ModelAutoModeResponse {
  enabled: boolean
  defaultAuto: boolean
  selected?: boolean
  autoSelected: boolean
  router: Omit<DecisionRouterDraft, 'apiKey'> & DecisionRouterInfo
  threshold: number
  minConfidence: number
  timeoutMs: number
  historyPrompts: number
  routes: ModelAutoRoute[]
  warnings?: string[]
}

export interface FieldError {
  /** Full path as sent by the server, e.g. `modelAutoMode.routes[0].description`. */
  field: string
  message: string
}

export interface RouterModelInfo {
  id: string
  name?: string
  contextWindow?: number
  capabilities?: string[]
}

export type RouterModelsStatus = 'filtered' | 'unfiltered' | 'unsupported'

export interface RouterModelsResponse {
  models: RouterModelInfo[]
  status: RouterModelsStatus
  hint?: string
  error?: string
  /** Suggested Ollama decision models that are not installed (Ollama only). */
  suggestions?: string[]
}

export interface RouterPullJob {
  id: string
  target: string
  state: 'running' | 'done' | 'error'
  status?: string
  completed: number
  total: number
  error?: string
}

export interface HealthReportDTO {
  ok: boolean
  kind?: string
  model?: string
  version?: string
  reachable: boolean
  authorized: boolean
  versionOK: boolean
  modelPresent: boolean
  isDecisionModel: boolean
  remote: boolean
  latencyMs: number
  problems?: string[]
}

export interface RouterTestResponse {
  ok: boolean
  problems?: string[]
  report?: HealthReportDTO
  error?: string
}

export interface PlaygroundDecision {
  routeId: string
  matched: boolean
  probability: number
  confidence: number
  probabilities?: Record<string, number>
  candidates?: string[]
  reason: string
  errClass?: string
  error?: string
  routerProvider?: string
  routerModel?: string
  latencyMs: number
  costUsd?: number
  inputTokens?: number
}

export interface PlaygroundResponse {
  decision: PlaygroundDecision
  state?: string
  usableCandidates?: string[]
  skipped?: Record<string, string>
}

export const DEFAULT_THRESHOLD = 0.6

export const EMPTY_DRAFT: ModelAutoModeDraft = {
  enabled: false,
  defaultAuto: false,
  router: { provider: 'ollama', baseURL: '', model: '', keepAlive: '', headers: {}, apiKey: '' },
  threshold: DEFAULT_THRESHOLD,
  minConfidence: 0,
  timeoutMs: 0,
  historyPrompts: 0,
  routes: [],
}

/** Extracts the server-provided body of an api error (`fetchApi` throws Error(bodyText)). */
export function parseApiError(err: unknown): { error: string; errors?: FieldError[] } {
  const raw = err instanceof Error ? err.message : String(err)
  try {
    const parsed = JSON.parse(raw) as { error?: string; errors?: FieldError[] }
    if (parsed && typeof parsed === 'object') {
      return { error: parsed.error || raw, errors: Array.isArray(parsed.errors) ? parsed.errors : undefined }
    }
  } catch {
    // not JSON
  }
  return { error: raw || 'unknown error' }
}

function fromResponse(r: ModelAutoModeResponse): { draft: ModelAutoModeDraft; info: DecisionRouterInfo } {
  return {
    draft: {
      enabled: r.enabled,
      defaultAuto: r.defaultAuto,
      router: {
        provider: r.router.provider || 'ollama',
        baseURL: r.router.baseURL ?? '',
        model: r.router.model ?? '',
        keepAlive: r.router.keepAlive ?? '',
        headers: { ...(r.router.headers ?? {}) },
        apiKey: '',
      },
      threshold: r.threshold || DEFAULT_THRESHOLD,
      minConfidence: r.minConfidence ?? 0,
      timeoutMs: r.timeoutMs ?? 0,
      historyPrompts: r.historyPrompts ?? 0,
      routes: (r.routes ?? []).map((x) => ({ ...x, fallbacks: [...(x.fallbacks ?? [])] })),
    },
    info: {
      effectiveBaseURL: r.router.effectiveBaseURL ?? '',
      apiKeySet: !!r.router.apiKeySet,
      apiKeyMasked: r.router.apiKeyMasked ?? '',
    },
  }
}

/** The router block sent to router/models and router/test drafts. */
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

interface ModelAutoModeStore {
  draft: ModelAutoModeDraft
  original: ModelAutoModeDraft
  info: DecisionRouterInfo
  /** Top-level flag: is Auto the active selection right now. */
  autoSelected: boolean
  /**
   * The concrete model the last prompt was routed to. Hook for a future
   * routing-notice event; nothing in this store invents an event name.
   */
  lastRoutedModel: string | null
  clearApiKey: boolean
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fieldErrors: FieldError[]
  warnings: string[]

  routerModels: RouterModelsResponse | null
  routerModelsLoading: boolean
  showAllModels: boolean

  /** Pull jobs by model name (latest state). */
  pulls: Record<string, RouterPullJob>

  testing: boolean
  testResult: RouterTestResponse | null

  playgroundRunning: boolean
  playground: PlaygroundResponse | null
  playgroundError: string | null

  fetchConfig: () => Promise<void>
  update: (patch: Partial<Omit<ModelAutoModeDraft, 'router'>>) => void
  updateRouter: (patch: Partial<DecisionRouterDraft>) => void
  setClearApiKey: (v: boolean) => void
  setRoutes: (routes: ModelAutoRoute[]) => void
  save: () => Promise<boolean>
  reset: () => void
  setLastRoutedModel: (model: string | null) => void
  setShowAllModels: (v: boolean) => void
  loadRouterModels: (showAll?: boolean) => Promise<void>
  pullModel: (model: string) => Promise<void>
  testConnection: () => Promise<void>
  runPlayground: (prompt: string, history: string[]) => Promise<void>
  /** Field errors for a path relative to the section (`routes[0].description`, `router.model`). */
  errorsFor: (field: string) => string[]
}

function isDirty(s: { draft: ModelAutoModeDraft; original: ModelAutoModeDraft; clearApiKey: boolean }) {
  return s.clearApiKey || JSON.stringify(s.draft) !== JSON.stringify(s.original)
}

export const useModelAutoModeStore = create<ModelAutoModeStore>((set, get) => ({
  draft: EMPTY_DRAFT,
  original: EMPTY_DRAFT,
  info: { effectiveBaseURL: '', apiKeySet: false, apiKeyMasked: '' },
  autoSelected: false,
  lastRoutedModel: null,
  clearApiKey: false,
  dirty: false,
  loading: false,
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
  playgroundRunning: false,
  playground: null,
  playgroundError: null,

  fetchConfig: async () => {
    set({ loading: true, error: null })
    try {
      const r = await api.get<ModelAutoModeResponse>('/api/v1/config/model-auto-mode')
      const { draft, info } = fromResponse(r)
      set({
        draft,
        original: draft,
        info,
        autoSelected: !!r.autoSelected,
        warnings: r.warnings ?? [],
        clearApiKey: false,
        dirty: false,
        fieldErrors: [],
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

  setRoutes: (routes) => get().update({ routes }),

  save: async () => {
    const { draft, clearApiKey } = get()
    set({ saving: true, error: null, fieldErrors: [] })
    try {
      const body: Record<string, unknown> = { ...draft, router: { ...draft.router } }
      if (clearApiKey) body.clearApiKey = true
      const r = await api.put<ModelAutoModeResponse>('/api/v1/config/model-auto-mode', body)
      const next = fromResponse(r)
      set({
        draft: next.draft,
        original: next.draft,
        info: next.info,
        autoSelected: !!r.autoSelected,
        warnings: r.warnings ?? [],
        clearApiKey: false,
        dirty: false,
      })
      useToastStore.getState().addToast('Auto mode settings saved', 'success')
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

  setLastRoutedModel: (model) => set({ lastRoutedModel: model }),

  setShowAllModels: (v) => set({ showAllModels: v }),

  loadRouterModels: async (showAll) => {
    const all = showAll ?? get().showAllModels
    set({ routerModelsLoading: true })
    try {
      const r = await api.post<RouterModelsResponse>('/api/v1/model-auto-mode/router/models', {
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
      let job = await api.post<RouterPullJob>('/api/v1/model-auto-mode/router/pull', {
        model,
        router: routerPayload(get().draft.router),
      })
      setPull(job)
      while (job.state === 'running') {
        await new Promise((r) => setTimeout(r, 1000))
        job = await api.get<RouterPullJob>(`/api/v1/model-auto-mode/router/pull/${job.id}`)
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
      const r = await api.post<RouterTestResponse>('/api/v1/model-auto-mode/router/test', {
        router: routerPayload(get().draft.router),
      })
      set({ testResult: r })
    } catch (e) {
      set({ testResult: { ok: false, error: parseApiError(e).error } })
    } finally {
      set({ testing: false })
    }
  },

  runPlayground: async (prompt, history) => {
    const { draft } = get()
    set({ playgroundRunning: true, playgroundError: null })
    try {
      const r = await api.post<PlaygroundResponse>('/api/v1/model-auto-mode/playground', {
        prompt,
        history,
        config: { ...draft, router: routerPayload(draft.router) },
      })
      set({ playground: r })
    } catch (e) {
      set({ playground: null, playgroundError: parseApiError(e).error })
    } finally {
      set({ playgroundRunning: false })
    }
  },

  errorsFor: (field) => {
    const full = `modelAutoMode.${field}`
    return get()
      .fieldErrors.filter((e) => e.field === full || e.field === field)
      .map((e) => e.message)
  },
}))
