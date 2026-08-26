import { create } from 'zustand'
import api, { getBaseURL } from '../services/api'
import { useToastStore } from './toastStore'

export type DesignKind = 'web' | 'deck'
export type ExportFormat = 'html' | 'png' | 'pdf'

export interface DesignArtifact {
  id: string
  session_id?: string
  title: string
  slug: string
  dir: string
  kind: DesignKind
  current_version: number
  created_at: string
  updated_at?: string
  /** Served preview address, or a file:// address when no preview server runs. */
  url?: string
  /** Preview address with the selection bridge enabled — what the canvas loads. */
  bridge_url?: string
  file_url?: string
  slides?: number
  entry?: string
}

export interface DesignVersion {
  artifact_id: string
  number: number
  snapshot_id: string
  summary: string
  created_at: string
  critique?: { score: number; summary?: string }
}

export interface DesignNode {
  node_id: string
  parent_id?: string
  selector?: string
  role?: string
  text?: string
  slide?: number
  box: { x: number; y: number; w: number; h: number }
  styles?: Record<string, string>
}

export interface DesignStatus {
  enabled: boolean
  preview: boolean
  preview_reason?: string
  renderer: boolean
  kinds: string[]
  output_dir?: string
}

/** The token set every artifact of the project links. */
export interface DesignSystem {
  name: string
  tokens: Record<string, Record<string, string>>
  fonts?: string[]
}

export interface DesignSystemPayload {
  system: DesignSystem
  /** False when the project never committed a system and these are defaults. */
  exists: boolean
  tokens_path: string
  stylesheet_path: string
  contract_path: string
}

export interface DesignSystemExample {
  name: string
  title: string
}

/**
 * DesignTemplate is one gallery entry: a design skill described by its `od:`
 * frontmatter block. Entries that are not startable (a workflow, a craft
 * reference) are still listed — they are part of the library, they just cannot
 * scaffold an artifact.
 */
export interface DesignTemplate {
  name: string
  description: string
  category: string
  scenario?: string
  example_prompt?: string
  mode?: string
  kind?: string
  preview?: string
  viewport?: { w: number; h: number }
  requires_system: boolean
  craft?: string[]
  critique_policy?: string
  startable: boolean
  source: 'bundled' | 'installed'
  installed: boolean
  source_path?: string
}

export type DesignExtractSource = 'code' | 'url' | 'image' | 'text'

export interface DesignExtractResult {
  system: DesignSystem
  source: DesignExtractSource
  target?: string
  scanned?: string[]
  notes?: string[]
}

/** One hardcoded value in an artifact that a token already covers. */
export interface DesignSystemFinding {
  file: string
  line: number
  property?: string
  value: string
  token: string
}

export interface DesignApplyResult {
  artifact_id: string
  system: string
  stylesheet: string
  linked: boolean
  entry: string
  findings?: DesignSystemFinding[]
  scanned: number
  truncated?: boolean
}

/** One finding against a version, from the audit or from the critic. */
export interface DesignIssue {
  code?: string
  severity: 'info' | 'warning' | 'error' | 'blocking'
  node_id?: string
  slide?: number
  message: string
  fix?: string
}

/** One recorded critic pass over a version. */
export interface DesignCritique {
  id: string
  artifact_id: string
  version: number
  score: number
  summary?: string
  issues: DesignIssue[]
  created_at: string
}

/** Why the quality gate stopped or kept going. */
export interface DesignGateDecision {
  pass: boolean
  iterate: boolean
  reason: string
  round: number
  max_rounds: number
  score: number
  threshold: number
  policy: string
  blocking: number
}

export interface DesignCritiqueSettings {
  enabled: boolean
  max_rounds: number
  threshold: number
  policy: string
}

/** A full critic pass: the evidence, the record and the decision. */
export interface DesignCritiqueReport {
  artifact: DesignArtifact
  version: number
  rendered: boolean
  audit: { score: number; summary: string; issues: DesignIssue[]; counts?: Record<string, number> }
  critique: DesignCritique
  decision: DesignGateDecision
  settings: DesignCritiqueSettings
  recorded: boolean
  render_error?: string
}

/** Selection reported by the preview bridge. */
export interface DesignSelection {
  nodeId: string
  selection: string
  tag?: string
  text?: string
  slide?: number
}

export interface DesignCreatedMarker {
  artifactId: string
  title?: string
  at: string
  nonce: number
}

interface DesignStore {
  status: DesignStatus | null
  artifacts: DesignArtifact[]
  loading: boolean
  activeId: string | null
  versions: DesignVersion[]
  nodes: DesignNode[]
  nodesTotal: number
  selection: DesignSelection | null
  slide: number
  rendering: boolean
  exporting: boolean
  /** Bumped on every version or render event; the canvas reloads on change. */
  reloadNonce: number
  lastCreated: DesignCreatedMarker | null
  connected: boolean
  _es: EventSource | null

  fetchStatus: () => Promise<void>
  fetchArtifacts: () => Promise<void>
  openArtifact: (id: string) => Promise<void>
  closeArtifact: () => void
  fetchVersions: (id: string) => Promise<void>
  fetchNodes: (id: string, opts?: { nodeId?: string; text?: string; slide?: number; styles?: boolean }) => Promise<void>
  render: (id: string) => Promise<void>
  checkout: (id: string, version: number) => Promise<void>
  exportArtifact: (id: string, format: ExportFormat, opts?: { slide?: number }) => Promise<string | null>
  setSelection: (selection: DesignSelection | null) => void
  setSlide: (slide: number) => void
  connect: () => void
  disconnect: () => void

  system: DesignSystemPayload | null
  systemExamples: DesignSystemExample[]
  systemLoading: boolean
  systemBusy: boolean
  /** Result of the last extraction, kept so a dry run can be reviewed. */
  lastExtraction: DesignExtractResult | null
  fetchSystem: () => Promise<void>
  fetchSystemExamples: () => Promise<void>
  saveSystemTokens: (
    tokens: Record<string, Record<string, string>>,
    opts?: { name?: string; fonts?: string[] },
  ) => Promise<void>
  extractSystem: (
    source: DesignExtractSource,
    target: string,
    opts?: { name?: string; dryRun?: boolean },
  ) => Promise<DesignExtractResult | null>
  applySystem: (id: string) => Promise<DesignApplyResult | null>

  critique: DesignCritique | null
  critiqueDecision: DesignGateDecision | null
  critiqueSettings: DesignCritiqueSettings | null
  critiqueRunning: boolean
  /** True once a critique has been read for the open artifact, so an empty
   * panel can tell "nothing recorded" from "not looked yet". */
  critiqueLoaded: boolean
  fetchCritique: (id: string) => Promise<void>
  runCritique: (id: string, opts?: { policy?: string }) => Promise<DesignCritiqueReport | null>

  templates: DesignTemplate[]
  craftReferences: string[]
  templatesLoading: boolean
  fetchTemplates: () => Promise<void>
  installTemplate: (name: string, scope?: 'project' | 'global') => Promise<boolean>
}

/** The artifact currently open, or undefined. */
export function activeArtifact(state: DesignStore): DesignArtifact | undefined {
  return state.artifacts.find((a) => a.id === state.activeId)
}

export const useDesignStore = create<DesignStore>((set, get) => ({
  status: null,
  artifacts: [],
  loading: false,
  activeId: null,
  versions: [],
  nodes: [],
  nodesTotal: 0,
  selection: null,
  slide: 0,
  rendering: false,
  exporting: false,
  reloadNonce: 0,
  lastCreated: null,
  connected: false,
  _es: null,
  system: null,
  systemExamples: [],
  systemLoading: false,
  systemBusy: false,
  lastExtraction: null,
  critique: null,
  critiqueDecision: null,
  critiqueSettings: null,
  critiqueRunning: false,
  critiqueLoaded: false,
  templates: [],
  craftReferences: [],
  templatesLoading: false,

  fetchCritique: async (id: string) => {
    try {
      const data = await api.get<{
        exists: boolean
        critique?: DesignCritique
        settings: DesignCritiqueSettings
        decision?: DesignGateDecision
      }>(`/api/v1/design/artifacts/${id}/critique`)
      set({
        critique: data.exists ? (data.critique ?? null) : null,
        critiqueDecision: data.decision ?? null,
        critiqueSettings: data.settings ?? null,
        critiqueLoaded: true,
      })
    } catch {
      // A panel with no critique is a valid state; an error here would only
      // replace an empty list with a red one.
      set({ critiqueLoaded: true })
    }
  },

  runCritique: async (id: string, opts = {}) => {
    set({ critiqueRunning: true })
    try {
      const report = await api.post<DesignCritiqueReport>(`/api/v1/design/artifacts/${id}/critique`, {
        policy: opts.policy,
      })
      set({
        critique: report.critique,
        critiqueDecision: report.decision,
        critiqueSettings: report.settings,
        critiqueLoaded: true,
      })
      if (!report.rendered && report.render_error) {
        // A pass that could not render scored only part of the artifact, and a
        // partial score read as a whole one is the failure mode worth a toast.
        useToastStore.getState().addToast(report.render_error, 'warning')
      }
      return report
    } catch (err) {
      useToastStore.getState().addToast(err instanceof Error ? err.message : String(err), 'error')
      return null
    } finally {
      set({ critiqueRunning: false })
    }
  },

  fetchTemplates: async () => {
    set({ templatesLoading: true })
    try {
      const data = await api.get<{ skills: DesignTemplate[]; craft: string[] }>(
        '/api/v1/design/skills',
      )
      set({ templates: data.skills ?? [], craftReferences: data.craft ?? [] })
    } catch {
      // The route is missing when the subsystem is off; an empty gallery says
      // so more honestly than a stale one.
      set({ templates: [], craftReferences: [] })
    } finally {
      set({ templatesLoading: false })
    }
  },

  installTemplate: async (name, scope = 'project') => {
    try {
      await api.post(`/api/v1/design/skills/${encodeURIComponent(name)}/install`, { scope })
      useToastStore.getState().addToast(`Installed ${name}`, 'success')
      await get().fetchTemplates()
      return true
    } catch (e) {
      useToastStore
        .getState()
        .addToast(e instanceof Error ? e.message : `Failed to install ${name}`, 'error')
      return false
    }
  },

  fetchSystem: async () => {
    set({ systemLoading: true })
    try {
      set({ system: await api.get<DesignSystemPayload>('/api/v1/design/system') })
    } catch {
      // The route is missing when the subsystem is off. Leaving the payload
      // null lets the panel say so rather than render an empty token table.
      set({ system: null })
    } finally {
      set({ systemLoading: false })
    }
  },

  fetchSystemExamples: async () => {
    try {
      const data = await api.get<{ examples: DesignSystemExample[] }>(
        '/api/v1/design/system/examples',
      )
      set({ systemExamples: data.examples ?? [] })
    } catch {
      set({ systemExamples: [] })
    }
  },

  saveSystemTokens: async (tokens, opts) => {
    set({ systemBusy: true })
    try {
      const payload = await api.put<DesignSystemPayload>('/api/v1/design/system', {
        name: opts?.name ?? '',
        tokens,
        fonts: opts?.fonts ?? [],
        replace_fonts: opts?.fonts !== undefined,
      })
      set({ system: payload })
      useToastStore.getState().addToast('Design system saved', 'success')
    } catch (e) {
      useToastStore
        .getState()
        .addToast(e instanceof Error ? e.message : 'Save failed', 'error')
    } finally {
      set({ systemBusy: false })
    }
  },

  extractSystem: async (source, target, opts) => {
    set({ systemBusy: true })
    try {
      const data = await api.post<{
        result: DesignExtractResult
        saved: boolean
        system?: DesignSystemPayload
        mirrored?: string
        mirror_error?: string
      }>('/api/v1/design/system/extract', {
        source,
        target,
        name: opts?.name ?? '',
        dry_run: opts?.dryRun ?? false,
      })
      set({ lastExtraction: data.result })
      if (data.system) set({ system: data.system })
      if (data.mirror_error) {
        useToastStore.getState().addToast(data.mirror_error, 'error')
      } else if (data.saved) {
        useToastStore
          .getState()
          .addToast(
            data.mirrored ? `Extracted and mirrored to ${data.mirrored}` : 'Design system extracted',
            'success',
          )
      }
      return data.result
    } catch (e) {
      useToastStore
        .getState()
        .addToast(e instanceof Error ? e.message : 'Extraction failed', 'error')
      return null
    } finally {
      set({ systemBusy: false })
    }
  },

  applySystem: async (id: string) => {
    set({ systemBusy: true })
    try {
      const result = await api.post<DesignApplyResult>(
        `/api/v1/design/artifacts/${id}/apply-system`,
        {},
      )
      useToastStore
        .getState()
        .addToast(
          result.linked
            ? `Linked ${result.stylesheet} from ${result.entry}`
            : `${result.entry} already links the design system`,
          'success',
        )
      set({ reloadNonce: get().reloadNonce + 1 })
      return result
    } catch (e) {
      useToastStore
        .getState()
        .addToast(e instanceof Error ? e.message : 'Apply failed', 'error')
      return null
    } finally {
      set({ systemBusy: false })
    }
  },

  fetchStatus: async () => {
    try {
      set({ status: await api.get<DesignStatus>('/api/v1/design/status') })
    } catch {
      // An older server has no status route. Treating that as "off" is right:
      // every other design route is missing there too.
      set({ status: { enabled: false, preview: false, renderer: false, kinds: [] } })
    }
  },

  fetchArtifacts: async () => {
    set({ loading: true })
    try {
      const data = await api.get<{ artifacts: DesignArtifact[] }>('/api/v1/design/artifacts')
      set({ artifacts: data.artifacts ?? [] })
    } catch {
      set({ artifacts: [] })
    } finally {
      set({ loading: false })
    }
  },

  openArtifact: async (id: string) => {
    set({
      activeId: id, selection: null, slide: 0, nodes: [], versions: [],
      critique: null, critiqueDecision: null, critiqueLoaded: false,
    })
    try {
      const artifact = await api.get<DesignArtifact>(`/api/v1/design/artifacts/${id}`)
      set((state) => ({
        artifacts: state.artifacts.some((a) => a.id === id)
          ? state.artifacts.map((a) => (a.id === id ? artifact : a))
          : [artifact, ...state.artifacts],
      }))
    } catch {
      // The gallery entry still carries enough to render a helpful empty state.
    }
    await Promise.all([get().fetchVersions(id), get().fetchNodes(id), get().fetchCritique(id)])
  },

  closeArtifact: () =>
    set({
      activeId: null, selection: null, nodes: [], versions: [], slide: 0,
      critique: null, critiqueDecision: null, critiqueLoaded: false,
    }),

  fetchVersions: async (id: string) => {
    try {
      const data = await api.get<{ versions: DesignVersion[] }>(`/api/v1/design/artifacts/${id}/versions`)
      // Newest first: the timeline reads top-down and the latest version is the
      // one a user acts on.
      const versions = (data.versions ?? []).slice().sort((a, b) => b.number - a.number)
      set({ versions })
    } catch {
      set({ versions: [] })
    }
  },

  fetchNodes: async (id: string, opts = {}) => {
    const params = new URLSearchParams()
    if (opts.nodeId) params.set('node_id', opts.nodeId)
    if (opts.text) params.set('text', opts.text)
    if (opts.slide !== undefined) params.set('slide', String(opts.slide))
    if (opts.styles) params.set('styles', '1')
    params.set('limit', '300')
    try {
      const data = await api.get<{ nodes: DesignNode[]; total: number }>(
        `/api/v1/design/artifacts/${id}/nodes?${params.toString()}`,
      )
      set({ nodes: data.nodes ?? [], nodesTotal: data.total ?? 0 })
    } catch {
      set({ nodes: [], nodesTotal: 0 })
    }
  },

  render: async (id: string) => {
    set({ rendering: true })
    try {
      await api.post(`/api/v1/design/artifacts/${id}/render`, {})
      await get().fetchNodes(id)
      set((s) => ({ reloadNonce: s.reloadNonce + 1 }))
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Render failed',
        'error',
      )
    } finally {
      set({ rendering: false })
    }
  },

  checkout: async (id: string, version: number) => {
    try {
      const artifact = await api.post<DesignArtifact>(`/api/v1/design/artifacts/${id}/checkout`, { version })
      set((state) => ({
        artifacts: state.artifacts.map((a) => (a.id === id ? artifact : a)),
        reloadNonce: state.reloadNonce + 1,
        selection: null,
      }))
      await Promise.all([get().fetchVersions(id), get().fetchNodes(id)])
      useToastStore.getState().addToast(`Checked out version ${version}`, 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Checkout failed',
        'error',
      )
    }
  },

  exportArtifact: async (id: string, format: ExportFormat, opts = {}) => {
    set({ exporting: true })
    try {
      const data = await api.post<{ export: { path: string; bytes: number; note?: string }; download_url: string }>(
        `/api/v1/design/artifacts/${id}/export`,
        { format, slide: opts.slide ?? 0 },
      )
      if (data.export?.note) {
        useToastStore.getState().addToast(data.export.note, 'info')
      } else {
        useToastStore.getState().addToast(`Exported ${format.toUpperCase()}`, 'success')
      }
      return data.download_url ?? null
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Export failed',
        'error',
      )
      return null
    } finally {
      set({ exporting: false })
    }
  },

  setSelection: (selection) => set({ selection }),
  setSlide: (slide) => set({ slide }),

  connect: () => {
    if (get()._es) return
    // EventSource cannot set headers, so the token travels as a query
    // parameter, the same way every other stream in this app does.
    const token = api.getToken()
    const base = getBaseURL()
    const url = token
      ? `${base}/api/v1/design/events?token=${encodeURIComponent(token)}`
      : `${base}/api/v1/design/events`
    const es = new EventSource(url)

    const onLifecycle = (raw: string) => {
      try {
        const event = JSON.parse(raw) as { kind: string; artifact_id: string; title?: string }
        const state = get()
        if (event.kind === 'design.created') {
          if (event.artifact_id) {
            set((current) => ({
              lastCreated: {
                artifactId: event.artifact_id,
                title: event.title,
                at: new Date().toISOString(),
                nonce: (current.lastCreated?.nonce ?? 0) + 1,
              },
            }))
          }
          void state.fetchArtifacts()
          return
        }
        if (event.artifact_id && event.artifact_id === state.activeId) {
          if (event.kind === 'design.version' || event.kind === 'design.render') {
            // A new version or a fresh render means the document on disk moved
            // under the open canvas: reload it and re-read the index the agent
            // just produced, or a click would resolve against stale ids.
            void state.fetchVersions(event.artifact_id)
            void state.fetchNodes(event.artifact_id)
            set((s) => ({ reloadNonce: s.reloadNonce + 1 }))
          }
          if (event.kind === 'design.critique') void state.fetchCritique(event.artifact_id)
        }
        void state.fetchArtifacts()
      } catch {
        // A malformed frame is not worth tearing the stream down for.
      }
    }

    es.onopen = () => set({ connected: true, _es: es })
    es.onmessage = (evt) => onLifecycle(evt.data)
    for (const kind of ['design.created', 'design.version', 'design.render', 'design.critique']) {
      es.addEventListener(kind, (evt) => onLifecycle((evt as MessageEvent).data))
    }
    es.onerror = () => {
      set({ connected: false })
      // EventSource reconnects on its own; keeping the handle avoids opening a
      // second stream on top of the retry.
    }
    set({ _es: es })
  },

  disconnect: () => {
    get()._es?.close()
    set({ _es: null, connected: false })
  },
}))
