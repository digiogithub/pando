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

/**
 * The editable block, same shape as the PUT body. The decision provider
 * (router, timeoutMs) lives in decisionModelStore and is never sent from here.
 */
export interface ModelAutoModeDraft {
  enabled: boolean
  defaultAuto: boolean
  threshold: number
  minConfidence: number
  historyPrompts: number
  routes: ModelAutoRoute[]
}

/** GET/PUT response. */
export interface ModelAutoModeResponse {
  enabled: boolean
  defaultAuto: boolean
  selected?: boolean
  autoSelected: boolean
  threshold: number
  minConfidence: number
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
  threshold: DEFAULT_THRESHOLD,
  minConfidence: 0,
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

function fromResponse(r: ModelAutoModeResponse): ModelAutoModeDraft {
  return {
    enabled: r.enabled,
    defaultAuto: r.defaultAuto,
    threshold: r.threshold || DEFAULT_THRESHOLD,
    minConfidence: r.minConfidence ?? 0,
    historyPrompts: r.historyPrompts ?? 0,
    routes: (r.routes ?? []).map((x) => ({ ...x, fallbacks: [...(x.fallbacks ?? [])] })),
  }
}

interface ModelAutoModeStore {
  draft: ModelAutoModeDraft
  original: ModelAutoModeDraft
  /** Top-level flag: is Auto the active selection right now. */
  autoSelected: boolean
  /**
   * The concrete model the last prompt was routed to, set from the routing
   * notice of each turn.
   */
  lastRoutedModel: string | null
  dirty: boolean
  loading: boolean
  saving: boolean
  error: string | null
  fieldErrors: FieldError[]
  warnings: string[]

  playgroundRunning: boolean
  playground: PlaygroundResponse | null
  playgroundError: string | null

  fetchConfig: () => Promise<void>
  update: (patch: Partial<ModelAutoModeDraft>) => void
  setRoutes: (routes: ModelAutoRoute[]) => void
  save: () => Promise<boolean>
  reset: () => void
  setLastRoutedModel: (model: string | null) => void
  /** Loads only whether Auto is the active selection (model chip at startup). */
  hydrateAutoSelected: () => Promise<void>
  runPlayground: (prompt: string, history: string[]) => Promise<void>
  /** Field errors for a path relative to the section (`routes[0].description`). */
  errorsFor: (field: string) => string[]
}

function isDirty(s: { draft: ModelAutoModeDraft; original: ModelAutoModeDraft }) {
  return JSON.stringify(s.draft) !== JSON.stringify(s.original)
}

export const useModelAutoModeStore = create<ModelAutoModeStore>((set, get) => ({
  draft: EMPTY_DRAFT,
  original: EMPTY_DRAFT,
  autoSelected: false,
  lastRoutedModel: null,
  dirty: false,
  loading: false,
  saving: false,
  error: null,
  fieldErrors: [],
  warnings: [],
  playgroundRunning: false,
  playground: null,
  playgroundError: null,

  fetchConfig: async () => {
    set({ loading: true, error: null })
    try {
      const r = await api.get<ModelAutoModeResponse>('/api/v1/config/model-auto-mode')
      const draft = fromResponse(r)
      set({
        draft,
        original: draft,
        autoSelected: !!r.autoSelected,
        warnings: r.warnings ?? [],
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
      return { draft, dirty: isDirty({ draft, original: s.original }) }
    }),

  setRoutes: (routes) => get().update({ routes }),

  save: async () => {
    const { draft } = get()
    set({ saving: true, error: null, fieldErrors: [] })
    try {
      // The decision provider is saved on its own page: no router/timeoutMs here.
      const r = await api.put<ModelAutoModeResponse>('/api/v1/config/model-auto-mode', { ...draft })
      const next = fromResponse(r)
      set({
        draft: next,
        original: next,
        autoSelected: !!r.autoSelected,
        warnings: r.warnings ?? [],
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

  reset: () => set((s) => ({ draft: s.original, dirty: false, fieldErrors: [], error: null })),

  setLastRoutedModel: (model) => set({ lastRoutedModel: model }),

  hydrateAutoSelected: async () => {
    try {
      const r = await api.get<ModelAutoModeResponse>('/api/v1/config/model-auto-mode')
      set({ autoSelected: !!r.autoSelected })
    } catch {
      // Older backend without model auto mode: keep the plain model label.
    }
  },

  runPlayground: async (prompt, history) => {
    const { draft } = get()
    set({ playgroundRunning: true, playgroundError: null })
    try {
      // Routes are the unsaved draft; the decision model is the saved one.
      const r = await api.post<PlaygroundResponse>('/api/v1/model-auto-mode/playground', {
        prompt,
        history,
        config: { ...draft },
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
