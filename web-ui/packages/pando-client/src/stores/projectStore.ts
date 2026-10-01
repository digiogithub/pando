import { create } from 'zustand'
import type { Project } from '../types'
import api, { getBaseURL } from '../services/api'
import { useToastStore } from './toastStore'

/** Workspace identity of the pando instance the UI is talking to. */
export interface Workspace {
  /** Absolute working directory the instance was started in. */
  cwd: string
  /** Pando version string reported by the server. */
  version: string
}

export type ProjectManagerEventType =
  | 'switched'
  | 'status_changed'
  | 'init_required'
  | 'delegation_changed'
  | 'web_started'
  | 'web_stopped'
  | 'web_error'

export interface ProjectManagerEvent {
  type: ProjectManagerEventType
  projectId: string
  status?: string
  error?: string
  delegations?: number
  webPort?: number
}

export type ProjectInitOutcome = 'initialized' | 'cancelled' | 'failed'

type ProjectEventListener = (event: ProjectManagerEvent) => void

const projectEventListeners = new Set<ProjectEventListener>()
const projectInitWaiters = new Map<string, Array<(outcome: ProjectInitOutcome) => void>>()

function emitProjectEvent(event: ProjectManagerEvent) {
  for (const listener of projectEventListeners) {
    listener(event)
  }
}

function resolveProjectInitWaiters(projectId: string, outcome: ProjectInitOutcome) {
  const waiters = projectInitWaiters.get(projectId)
  if (!waiters || waiters.length === 0) return
  projectInitWaiters.delete(projectId)
  for (const resolve of waiters) {
    resolve(outcome)
  }
}

export function registerProjectEventListener(listener: ProjectEventListener): () => void {
  projectEventListeners.add(listener)
  return () => {
    projectEventListeners.delete(listener)
  }
}

export function waitForProjectInit(projectId: string): Promise<ProjectInitOutcome> {
  return new Promise((resolve) => {
    const waiters = projectInitWaiters.get(projectId) ?? []
    waiters.push(resolve)
    projectInitWaiters.set(projectId, waiters)
  })
}

function addProjectToast(message: string, type: 'success' | 'error' | 'warning' | 'info') {
  useToastStore.getState().addToast(message, type)
}

function addProjectToastKey(
  key: string,
  type: 'success' | 'error' | 'warning' | 'info',
  values?: Record<string, unknown>,
) {
  useToastStore.getState().addToastKey(key, type, values)
}

interface ProjectStore {
  projects: Project[]
  activeProjectId: string | null
  /** Working directory + version of the connected instance; null until fetched. */
  workspace: Workspace | null
  loading: boolean
  initDialogProject: Project | null
  _es: EventSource | null

  fetchProjects: () => Promise<void>
  fetchActive: () => Promise<void>
  fetchWorkspace: () => Promise<void>
  addProject: (path: string, name?: string) => Promise<void>
  activateProject: (id: string) => Promise<'ok' | 'needs_init'>
  stopProject: (id: string) => Promise<boolean>
  openProjectDesktop: (id: string) => Promise<void>
  deactivateProject: () => Promise<void>
  initProject: (id: string, options?: { activateAfter?: boolean }) => Promise<boolean>
  renameProject: (id: string, name: string) => Promise<boolean>
  removeProject: (id: string) => Promise<void>
  setInitDialogProject: (p: Project | null) => void
  connectEvents: () => void
  disconnectEvents: () => void
}

let _backoffMs = 1_000
const MAX_BACKOFF_MS = 30_000

export const useProjectStore = create<ProjectStore>((set, get) => ({
  projects: [],
  activeProjectId: null,
  workspace: null,
  loading: false,
  initDialogProject: null,
  _es: null,

  fetchProjects: async () => {
    set({ loading: true })
    try {
      const data = await api.get<{ projects: Project[] }>('/api/v1/projects')
      set({ projects: data.projects ?? [] })
    } catch {
      set({ projects: [] })
    } finally {
      set({ loading: false })
    }
  },

  fetchActive: async () => {
    try {
      const data = await api.get<{ project: Project | null }>('/api/v1/projects/active')
      set({ activeProjectId: data.project?.id ?? null })
    } catch {
      set({ activeProjectId: null })
    }
  },

  // The instance's own working directory (the TUI shows it as "cwd:" under the
  // chat). It only changes when pando is restarted or the web UI is pointed at a
  // different instance, so one fetch per mount is enough.
  fetchWorkspace: async () => {
    try {
      const data = await api.get<{ cwd?: string; version?: string }>('/api/v1/project')
      set({ workspace: { cwd: data.cwd ?? '', version: data.version ?? '' } })
    } catch {
      set({ workspace: null })
    }
  },

  addProject: async (path: string, name?: string) => {
    try {
      await api.post('/api/v1/projects', { path, name: name ?? '' })
      await get().fetchProjects()
      addProjectToastKey('projects.toasts.added', 'success', { path })
    } catch (e) {
      addProjectToast(e instanceof Error ? e.message : 'Failed to add project', 'error')
    }
  },

  activateProject: async (id: string): Promise<'ok' | 'needs_init'> => {
    try {
      await api.post(`/api/v1/projects/${id}/activate`, {})
      await Promise.all([get().fetchActive(), get().fetchProjects()])
      addProjectToastKey('projects.toasts.activated', 'success')
      return 'ok'
    } catch (e) {
      if (e instanceof Error) {
        // The api service throws new Error(responseBody) for non-2xx.
        // A 409 "project_needs_init" response body is JSON we can parse.
        try {
          const body = JSON.parse(e.message) as { error?: string; project_id?: string }
          if (body.error === 'project_needs_init') {
            // Find the project from local state to pass to the init dialog.
            const proj = get().projects.find((p) => p.id === id) ?? null
            set({ initDialogProject: proj })
            return 'needs_init'
          }
        } catch {
          // Not JSON — fall through to generic error handling.
        }
        addProjectToast(e.message || 'Failed to activate project', 'error')
      }
      return 'ok'
    }
  },

  stopProject: async (id: string) => {
    try {
      const resp = await api.post<{ cancelled_delegations?: number }>(`/api/v1/projects/${id}/stop`, {})
      await Promise.all([get().fetchActive(), get().fetchProjects()])
      const cancelled = resp?.cancelled_delegations ?? 0
      if (cancelled > 0) {
        addProjectToastKey('projects.toasts.cancelledDelegations', 'info', { count: cancelled })
      } else {
        addProjectToastKey('projects.toasts.stopped', 'success')
      }
      return true
    } catch (e) {
      if (e instanceof Error) {
        // A 409 "external_instance" response body is JSON we can parse.
        try {
          const body = JSON.parse(e.message) as { error?: string }
          if (body.error === 'external_instance') {
            addProjectToastKey('projects.toasts.externalStopBlocked', 'error')
            // Refresh so the UI stays in sync with the still-running instance.
            void get().fetchProjects()
            return false
          }
        } catch {
          // Not JSON — fall through to generic error handling.
        }
        addProjectToast(e.message || 'Failed to stop project', 'error')
      }
      return false
    }
  },

  // Desktop app only: open the project in its own Pando desktop window (a
  // separate `pando desktop --cwd <path>` process).
  openProjectDesktop: async (id: string) => {
    try {
      const resp = await api.post<{ status?: string }>(`/api/v1/projects/${id}/open-desktop`, {})
      const toasts = useToastStore.getState()
      switch (resp?.status) {
        case 'current':
          toasts.addToastKey('projects.toasts.desktopCurrent', 'info')
          break
        case 'already_open':
          toasts.addToastKey('projects.toasts.desktopAlreadyOpen', 'info')
          break
        default:
          toasts.addToastKey('projects.toasts.desktopOpening', 'success')
      }
    } catch (e) {
      let message = e instanceof Error ? e.message : ''
      try {
        message = (JSON.parse(message) as { error?: string }).error ?? message
      } catch {
        // Not JSON — keep the raw message.
      }
      addProjectToast(message || 'Failed to open project window', 'error')
    }
  },

  deactivateProject: async () => {
    const { activeProjectId } = get()
    if (!activeProjectId) return
    try {
      await api.post(`/api/v1/projects/${activeProjectId}/deactivate`, {})
      await Promise.all([get().fetchActive(), get().fetchProjects()])
      addProjectToastKey('projects.toasts.deactivated', 'success')
    } catch (e) {
      addProjectToast(e instanceof Error ? e.message : 'Failed to deactivate project', 'error')
    }
  },

  initProject: async (id: string, options?: { activateAfter?: boolean }) => {
    try {
      await api.post(`/api/v1/projects/${id}/init`, {})
      await get().fetchProjects()
      if (options?.activateAfter) {
        const activation = await get().activateProject(id)
        if (activation === 'needs_init') {
          resolveProjectInitWaiters(id, 'failed')
          return false
        }
      }
      resolveProjectInitWaiters(id, 'initialized')
      addProjectToastKey('projects.toasts.initialized', 'success')
      return true
    } catch (e) {
      resolveProjectInitWaiters(id, 'failed')
      addProjectToast(e instanceof Error ? e.message : 'Failed to initialize project', 'error')
      return false
    }
  },

  renameProject: async (id: string, name: string) => {
    try {
      const response = await api.patch<{ project?: Project }>(`/api/v1/projects/${id}`, { name })
      const renamed = response.project
      if (renamed) {
        set((state) => ({
          projects: state.projects.map((project) => (project.id === id ? renamed : project)),
        }))
      } else {
        await get().fetchProjects()
      }
      addProjectToastKey('projects.toasts.renamed', 'success', { name })
      return true
    } catch (e) {
      addProjectToast(e instanceof Error ? e.message : 'Failed to rename project', 'error')
      return false
    }
  },

  removeProject: async (id: string) => {
    try {
      await api.delete(`/api/v1/projects/${id}`)
      await get().fetchProjects()
      // Clear active project if it was the removed one.
      if (get().activeProjectId === id) {
        set({ activeProjectId: null })
      }
      addProjectToastKey('projects.toasts.removed', 'success')
    } catch (e) {
      addProjectToast(e instanceof Error ? e.message : 'Failed to remove project', 'error')
    }
  },

  setInitDialogProject: (p: Project | null) => {
    const current = get().initDialogProject
    if (p === null && current) {
      resolveProjectInitWaiters(current.id, 'cancelled')
    }
    set({ initDialogProject: p })
  },

  connectEvents: () => {
    if (get()._es) return

    const open = () => {
      const token = api.getToken()
      const base = getBaseURL()
      const url = token
        ? `${base}/api/v1/projects/events?token=${encodeURIComponent(token)}`
        : `${base}/api/v1/projects/events`
      const es = new EventSource(url)

      es.onopen = () => {
        _backoffMs = 1_000
        set({ _es: es })
      }

      const refresh = () => {
        void get().fetchProjects()
        void get().fetchActive()
      }

      const parseEvent = (type: ProjectManagerEventType, raw: string): ProjectManagerEvent => {
        let data: {
          project_id?: string
          status?: string
          error?: string
          delegations?: number
          web_port?: number
        } = {}

        try {
          data = JSON.parse(raw) as typeof data
        } catch {
          // Keep the event with an empty payload when the server sends no data.
        }

        const parsed: ProjectManagerEvent = {
          type,
          projectId: data.project_id ?? '',
          status: data.status,
          error: data.error,
          delegations: data.delegations,
          webPort: data.web_port,
        }
        emitProjectEvent(parsed)
        return parsed
      }

      const bindEvent = (
        type: ProjectManagerEventType,
        handler: (event: ProjectManagerEvent) => void,
      ) => {
        es.addEventListener(type, (event) => {
          const payload = event instanceof MessageEvent ? event.data : ''
          const parsed = parseEvent(type, typeof payload === 'string' ? payload : '')
          handler(parsed)
        })
      }

      bindEvent('switched', () => {
        refresh()
      })
      bindEvent('status_changed', () => {
        refresh()
      })
      bindEvent('delegation_changed', () => {
        // In-flight delegated-loop count changed — refresh the row badges.
        void get().fetchProjects()
      })
      bindEvent('init_required', () => {
        void get().fetchProjects()
      })
      bindEvent('web_started', () => {
        void get().fetchProjects()
      })
      bindEvent('web_stopped', () => {
        void get().fetchProjects()
      })
      bindEvent('web_error', () => {
        void get().fetchProjects()
      })

      es.onerror = () => {
        es.close()
        set({ _es: null })
        setTimeout(() => {
          _backoffMs = Math.min(_backoffMs * 2, MAX_BACKOFF_MS)
          open()
        }, _backoffMs)
      }
    }

    open()
  },

  disconnectEvents: () => {
    const { _es } = get()
    if (_es) {
      _es.close()
      set({ _es: null })
    }
  },
}))
