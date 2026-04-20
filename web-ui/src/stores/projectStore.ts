import { create } from 'zustand'
import type { Project } from '@/types'
import api, { getBaseURL } from '@/services/api'
import { useToastStore } from './toastStore'

interface ProjectStore {
  projects: Project[]
  activeProjectId: string | null
  loading: boolean
  initDialogProject: Project | null
  _es: EventSource | null

  fetchProjects: () => Promise<void>
  fetchActive: () => Promise<void>
  addProject: (path: string, name?: string) => Promise<void>
  activateProject: (id: string) => Promise<'ok' | 'needs_init'>
  deactivateProject: () => Promise<void>
  initProject: (id: string) => Promise<void>
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

  addProject: async (path: string, name?: string) => {
    try {
      await api.post('/api/v1/projects', { path, name: name ?? '' })
      await get().fetchProjects()
      useToastStore.getState().addToast(`Project added: ${path}`, 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to add project',
        'error',
      )
    }
  },

  activateProject: async (id: string): Promise<'ok' | 'needs_init'> => {
    try {
      await api.post(`/api/v1/projects/${id}/activate`, {})
      await Promise.all([get().fetchActive(), get().fetchProjects()])
      useToastStore.getState().addToast('Project activated', 'success')
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
        useToastStore.getState().addToast(e.message || 'Failed to activate project', 'error')
      }
      return 'ok'
    }
  },

  deactivateProject: async () => {
    const { activeProjectId } = get()
    if (!activeProjectId) return
    try {
      await api.post(`/api/v1/projects/${activeProjectId}/deactivate`, {})
      await Promise.all([get().fetchActive(), get().fetchProjects()])
      useToastStore.getState().addToast('Project deactivated', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to deactivate project',
        'error',
      )
    }
  },

  initProject: async (id: string) => {
    try {
      await api.post(`/api/v1/projects/${id}/init`, {})
      await get().fetchProjects()
      await get().activateProject(id)
      useToastStore.getState().addToast('Project initialized', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to initialize project',
        'error',
      )
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
      useToastStore.getState().addToast('Project removed', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to remove project',
        'error',
      )
    }
  },

  setInitDialogProject: (p: Project | null) => set({ initDialogProject: p }),

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

      es.addEventListener('switched', refresh)
      es.addEventListener('status_changed', refresh)
      es.addEventListener('init_required', () => {
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
