import { create } from 'zustand'
import api, { isProjectChildMode } from '../services/api'
import { localBrowserStorage } from '../services/storage'
import type { Project, ProjectTab, ProjectWebInstance } from '../types'
import {
  registerProjectEventListener,
  useProjectStore,
  waitForProjectInit,
  type ProjectManagerEvent,
} from './projectStore'
import type { ToastType } from './toastStore'

const PROJECT_TABS_STORAGE_KEY = 'pando_project_tabs'
const DEFAULT_MAIN_ROUTE = '/'

interface PersistedProjectTabsState {
  order?: string[]
  activeTabId?: 'main' | string
  lastMainRoute?: string
}

export interface ProjectTabNotice {
  key: string
  type: ToastType
}

export interface ProjectTabActionResult {
  ok: boolean
  code:
    | 'opened'
    | 'already_open'
    | 'closed'
    | 'removed'
    | 'cancelled'
    | 'noop'
    | 'error'
  tab?: ProjectTab
  notice?: ProjectTabNotice
  error?: string
}

interface ProjectTabsStore {
  tabs: ProjectTab[]
  activeTabId: 'main' | string
  focusRequestId: number
  order: string[]
  lastMainRoute: string
  openTab: (projectId: string) => Promise<ProjectTabActionResult>
  closeTab: (projectId: string, options?: { stop?: boolean }) => Promise<ProjectTabActionResult>
  focusTab: (id: 'main' | string) => void
  setRuntimeState: (
    projectId: string,
    runtime: { busy?: boolean; childTitle?: string | null },
  ) => void
  reorder: (order: string[]) => void
  restore: () => Promise<void>
  applyEvent: (event: ProjectManagerEvent) => void
  restartTab: (projectId: string) => Promise<ProjectTabActionResult>
}

interface ProjectOpenResponse {
  status: 'opened' | 'already_open'
  project_id: string
  web_url?: string
  web_port?: number
}

interface ProjectCloseResponse {
  status: 'closed'
  project_id: string
  cancelled_delegations?: number
}

interface ProjectTabsErrorBody {
  error?: string
  detail?: string
  path?: string
}

const inflightOpens = new Map<string, Promise<ProjectTabActionResult>>()

function projectTabWebURL(projectId: string): string {
  return `/api/v1/projects/${encodeURIComponent(projectId)}/web/`
}

function readPersistedState(): PersistedProjectTabsState {
  const raw = localBrowserStorage.getItem(PROJECT_TABS_STORAGE_KEY)
  if (!raw) return {}

  try {
    const parsed = JSON.parse(raw) as PersistedProjectTabsState
    return {
      order: Array.isArray(parsed.order)
        ? parsed.order.filter((value): value is string => typeof value === 'string')
        : [],
      activeTabId: typeof parsed.activeTabId === 'string' ? parsed.activeTabId : 'main',
      lastMainRoute:
        typeof parsed.lastMainRoute === 'string' && parsed.lastMainRoute.trim()
          ? parsed.lastMainRoute
          : DEFAULT_MAIN_ROUTE,
    }
  } catch {
    return {}
  }
}

function writePersistedState(state: Pick<ProjectTabsStore, 'activeTabId' | 'order' | 'lastMainRoute'>) {
  if (isProjectChildMode()) return

  localBrowserStorage.setItem(
    PROJECT_TABS_STORAGE_KEY,
    JSON.stringify({
      order: state.order,
      activeTabId: state.activeTabId,
      lastMainRoute: state.lastMainRoute,
    } satisfies PersistedProjectTabsState),
  )
}

function parseProjectTabsError(error: unknown): ProjectTabsErrorBody {
  if (!(error instanceof Error)) return {}

  try {
    return JSON.parse(error.message) as ProjectTabsErrorBody
  } catch {
    return { detail: error.message }
  }
}

function findProject(projectId: string): Project | null {
  return useProjectStore.getState().projects.find((project) => project.id === projectId) ?? null
}

function baseTab(projectId: string, project: Project | null, existing?: ProjectTab): ProjectTab {
  return {
    projectId,
    name: project?.name ?? existing?.name ?? projectId,
    path: project?.path ?? existing?.path ?? '',
    state: existing?.state ?? 'starting',
    webUrl: existing?.webUrl ?? project?.web_url ?? projectTabWebURL(projectId),
    webPort: existing?.webPort ?? project?.web_port,
    delegations: existing?.delegations ?? project?.delegations ?? 0,
    openedAt: existing?.openedAt ?? new Date().toISOString(),
    error: existing?.error,
    busy: existing?.busy ?? false,
    childTitle: existing?.childTitle,
  }
}

function mergeOrder(order: string[], tabs: ProjectTab[]): string[] {
  const seen = new Set<string>()
  const merged: string[] = []

  for (const projectId of order) {
    if (seen.has(projectId)) continue
    if (tabs.some((tab) => tab.projectId === projectId)) {
      merged.push(projectId)
      seen.add(projectId)
    }
  }

  for (const tab of tabs) {
    if (seen.has(tab.projectId)) continue
    merged.push(tab.projectId)
    seen.add(tab.projectId)
  }

  return merged
}

function sortTabs(tabs: ProjectTab[], order: string[]): ProjectTab[] {
  const rank = new Map(order.map((projectId, index) => [projectId, index]))
  return [...tabs].sort((left, right) => {
    const leftRank = rank.get(left.projectId) ?? Number.MAX_SAFE_INTEGER
    const rightRank = rank.get(right.projectId) ?? Number.MAX_SAFE_INTEGER
    if (leftRank !== rightRank) return leftRank - rightRank
    return left.name.localeCompare(right.name)
  })
}

function upsertTab(tabs: ProjectTab[], nextTab: ProjectTab): ProjectTab[] {
  const existingIndex = tabs.findIndex((tab) => tab.projectId === nextTab.projectId)
  if (existingIndex === -1) {
    return [...tabs, nextTab]
  }

  return tabs.map((tab, index) => (index === existingIndex ? nextTab : tab))
}

function removeTab(tabs: ProjectTab[], projectId: string): ProjectTab[] {
  return tabs.filter((tab) => tab.projectId !== projectId)
}

function projectWebInstanceToTab(instance: ProjectWebInstance): ProjectTab {
  return {
    projectId: instance.project_id,
    name: instance.name,
    path: instance.path,
    state: instance.state,
    webUrl: instance.web_url ?? projectTabWebURL(instance.project_id),
    webPort: instance.web_port,
    delegations: instance.delegations ?? 0,
    openedAt: instance.started_at,
    error: instance.error,
  }
}

function normalizeActiveTabId(activeTabId: 'main' | string, tabs: ProjectTab[]): 'main' | string {
  if (activeTabId === 'main') return 'main'
  return tabs.some((tab) => tab.projectId === activeTabId) ? activeTabId : 'main'
}

async function ensureProject(projectId: string): Promise<Project | null> {
  const current = findProject(projectId)
  if (current) return current

  await useProjectStore.getState().fetchProjects()
  return findProject(projectId)
}

async function startTab(projectId: string, force: boolean): Promise<ProjectTabActionResult> {
  if (isProjectChildMode()) {
    return { ok: true, code: 'noop' }
  }

  const store = useProjectTabsStore.getState()
  const currentTab = store.tabs.find((tab) => tab.projectId === projectId)
  if (!force && currentTab && (currentTab.state === 'starting' || currentTab.state === 'running')) {
    store.focusTab(projectId)
    return {
      ok: true,
      code: 'already_open',
      tab: currentTab,
      notice: { key: 'projects.tabs.started', type: 'success' },
    }
  }

  const inflight = inflightOpens.get(projectId)
  if (inflight) {
    return inflight
  }

  const promise: Promise<ProjectTabActionResult> = (async (): Promise<ProjectTabActionResult> => {
    const project = await ensureProject(projectId)
    if (!project) {
      return {
        ok: false,
        code: 'error',
        error: 'project_not_found',
      }
    }

    while (true) {
      const existing = useProjectTabsStore.getState().tabs.find((tab) => tab.projectId === projectId)
      const startingTab: ProjectTab = {
        ...baseTab(projectId, project, existing),
        state: 'starting',
        error: undefined,
      }

      useProjectTabsStore.setState((state) => {
        const tabs = sortTabs(
          upsertTab(state.tabs, startingTab),
          mergeOrder(state.order, upsertTab(state.tabs, startingTab)),
        )
        return {
          tabs,
          order: mergeOrder(state.order, tabs),
        }
      })

      try {
        const response = await api.post<ProjectOpenResponse>(
          `/api/v1/projects/${projectId}/web/open`,
          {},
        )

        const openedTab: ProjectTab = {
          ...baseTab(projectId, project, startingTab),
          state: 'running',
          webUrl: response.web_url ?? startingTab.webUrl,
          webPort: response.web_port ?? startingTab.webPort,
          error: undefined,
          busy: false,
        }

        useProjectTabsStore.setState((state) => {
          const tabs = sortTabs(
            upsertTab(state.tabs, openedTab),
            mergeOrder(state.order, upsertTab(state.tabs, openedTab)),
          )
          return {
            tabs,
            order: mergeOrder(state.order, tabs),
            activeTabId: projectId,
          }
        })

        return {
          ok: true,
          code: response.status,
          tab: openedTab,
          notice: { key: 'projects.tabs.started', type: 'success' },
        }
      } catch (error) {
        const parsed = parseProjectTabsError(error)

        if (parsed.error === 'project_needs_init') {
          useProjectStore.getState().setInitDialogProject(project ?? baseProject(projectId, currentTab))
          const outcome = await waitForProjectInit(projectId)
          if (outcome === 'initialized') {
            continue
          }

          const nextTab: ProjectTab = {
            ...baseTab(projectId, project, currentTab),
            state: 'stopped',
            error: undefined,
            busy: false,
            childTitle: undefined,
          }

          useProjectTabsStore.setState((state) => ({
            tabs: sortTabs(upsertTab(state.tabs, nextTab), mergeOrder(state.order, upsertTab(state.tabs, nextTab))),
            order: mergeOrder(state.order, upsertTab(state.tabs, nextTab)),
          }))

          return {
            ok: false,
            code: outcome === 'cancelled' ? 'cancelled' : 'error',
            tab: nextTab,
            notice:
              outcome === 'cancelled'
                ? undefined
                : { key: 'projects.tabs.needsInit', type: 'warning' },
          }
        }

        const detail = parsed.detail ?? parsed.error ?? (error instanceof Error ? error.message : '')
        const erroredTab: ProjectTab = {
          ...baseTab(projectId, project, currentTab),
          state: 'error',
          error: detail,
          busy: false,
          childTitle: undefined,
        }

        useProjectTabsStore.setState((state) => ({
          tabs: sortTabs(upsertTab(state.tabs, erroredTab), mergeOrder(state.order, upsertTab(state.tabs, erroredTab))),
          order: mergeOrder(state.order, upsertTab(state.tabs, erroredTab)),
        }))

        return {
          ok: false,
          code: 'error',
          tab: erroredTab,
          error: detail,
          notice: { key: 'projects.tabs.error', type: 'error' },
        }
      }
    }
  })().finally(() => {
    inflightOpens.delete(projectId)
  })

  inflightOpens.set(projectId, promise)
  return promise
}

function baseProject(projectId: string, tab?: ProjectTab): Project {
  return {
    id: projectId,
    name: tab?.name ?? projectId,
    path: tab?.path ?? '',
    status: 'stopped',
    initialized: true,
    created_at: 0,
    updated_at: 0,
  }
}

export const useProjectTabsStore = create<ProjectTabsStore>((set, get) => ({
  tabs: [],
  activeTabId: 'main',
  focusRequestId: 0,
  order: [],
  lastMainRoute: DEFAULT_MAIN_ROUTE,

  openTab: async (projectId) => startTab(projectId, false),

  closeTab: async (projectId, options) => {
    if (isProjectChildMode()) {
      return { ok: true, code: 'noop' }
    }

    const current = get().tabs.find((tab) => tab.projectId === projectId)
    if (!current) {
      return { ok: true, code: 'noop' }
    }

    if (options?.stop) {
      try {
        await api.post<ProjectCloseResponse>(`/api/v1/projects/${projectId}/web/close`, {})
      } catch (error) {
        const parsed = parseProjectTabsError(error)
        const detail = parsed.detail ?? parsed.error ?? (error instanceof Error ? error.message : '')
        const erroredTab: ProjectTab = {
          ...current,
          state: 'error',
          error: detail,
        }
        set((state) => ({
          tabs: sortTabs(upsertTab(state.tabs, erroredTab), mergeOrder(state.order, upsertTab(state.tabs, erroredTab))),
        }))
        return {
          ok: false,
          code: 'error',
          tab: erroredTab,
          error: detail,
          notice: { key: 'projects.tabs.error', type: 'error' },
        }
      }
    }

    set((state) => {
      const tabs = removeTab(state.tabs, projectId)
      const order = state.order.filter((id) => id !== projectId)
      return {
        tabs,
        order,
        activeTabId:
          state.activeTabId === projectId ? normalizeActiveTabId('main', tabs) : normalizeActiveTabId(state.activeTabId, tabs),
      }
    })

    return {
      ok: true,
      code: options?.stop ? 'closed' : 'removed',
      notice: options?.stop ? { key: 'projects.tabs.stopped', type: 'info' } : undefined,
    }
  },

  focusTab: (id) => {
    if (id !== 'main' && !get().tabs.some((tab) => tab.projectId === id)) {
      return
    }
    set((state) => ({ activeTabId: id, focusRequestId: state.focusRequestId + 1 }))
  },

  setRuntimeState: (projectId, runtime) => {
    set((state) => ({
      tabs: state.tabs.map((tab) =>
        tab.projectId === projectId
          ? {
              ...tab,
              busy: runtime.busy ?? tab.busy ?? false,
              childTitle:
                runtime.childTitle === undefined
                  ? tab.childTitle
                  : runtime.childTitle || undefined,
            }
          : tab,
      ),
    }))
  },

  reorder: (nextOrder) => {
    const tabs = get().tabs
    const order = mergeOrder(nextOrder, tabs)
    set({
      tabs: sortTabs(tabs, order),
      order,
      activeTabId: normalizeActiveTabId(get().activeTabId, tabs),
    })
  },

  restore: async () => {
    if (isProjectChildMode()) {
      set({
        tabs: [],
        order: [],
        activeTabId: 'main',
        focusRequestId: 0,
        lastMainRoute: DEFAULT_MAIN_ROUTE,
      })
      return
    }

    const persisted = readPersistedState()
    const response = await api.get<{ instances: ProjectWebInstance[] }>('/api/v1/projects/web')
    const tabs = (response.instances ?? []).map(projectWebInstanceToTab)
    const order = mergeOrder(persisted.order ?? [], tabs)

    set({
      tabs: sortTabs(tabs, order),
      order,
      activeTabId: normalizeActiveTabId(persisted.activeTabId ?? 'main', tabs),
      focusRequestId: 0,
      lastMainRoute: persisted.lastMainRoute ?? DEFAULT_MAIN_ROUTE,
    })
  },

  applyEvent: (event) => {
    if (isProjectChildMode() || !event.projectId) return

    if (event.type === 'delegation_changed') {
      set((state) => ({
        tabs: state.tabs.map((tab) =>
          tab.projectId === event.projectId
            ? { ...tab, delegations: event.delegations ?? 0 }
            : tab,
        ),
      }))
      return
    }

    if (event.type === 'status_changed') {
      if (!['starting', 'running', 'error', 'stopped'].includes(event.status ?? '')) {
        return
      }
      set((state) => ({
        tabs: state.tabs.map((tab) =>
          tab.projectId === event.projectId
            ? {
                ...tab,
                state: event.status as ProjectTab['state'],
                error: event.status === 'error' ? event.error ?? tab.error : undefined,
                busy: event.status === 'running' ? tab.busy ?? false : false,
                childTitle: event.status === 'running' ? tab.childTitle : undefined,
              }
            : tab,
        ),
      }))
      return
    }

    set((state) => {
      const existing = state.tabs.find((tab) => tab.projectId === event.projectId)
      const project = findProject(event.projectId)
      const base = baseTab(event.projectId, project, existing)
      let nextTab: ProjectTab

      switch (event.type) {
        case 'web_started':
          nextTab = {
            ...base,
            state: 'running',
            webPort: event.webPort ?? base.webPort,
            error: undefined,
            busy: false,
          }
          break
        case 'web_stopped':
          if (!existing) return state
          nextTab = {
            ...base,
            state: 'stopped',
            webPort: event.webPort ?? base.webPort,
            delegations: 0,
            error: undefined,
            busy: false,
            childTitle: undefined,
          }
          break
        case 'web_error':
          nextTab = {
            ...base,
            state: 'error',
            webPort: event.webPort ?? base.webPort,
            error: event.error ?? base.error,
            busy: false,
            childTitle: undefined,
          }
          break
        default:
          return state
      }

      const tabs = sortTabs(upsertTab(state.tabs, nextTab), mergeOrder(state.order, upsertTab(state.tabs, nextTab)))
      return {
        tabs,
        order: mergeOrder(state.order, tabs),
        activeTabId: normalizeActiveTabId(state.activeTabId, tabs),
      }
    })
  },

  restartTab: async (projectId) => startTab(projectId, true),
}))

useProjectTabsStore.subscribe((state) => {
  writePersistedState(state)
})

registerProjectEventListener((event) => {
  useProjectTabsStore.getState().applyEvent(event)
})
