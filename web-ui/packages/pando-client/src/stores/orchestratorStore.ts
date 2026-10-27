import { create } from 'zustand'
import type { OrchestratorTask, DelegationMetrics } from '../types'
import api from '../services/api'

interface OrchestratorStore {
  tasks: OrchestratorTask[]
  selectedTask: OrchestratorTask | null
  loading: boolean
  createDialogOpen: boolean
  delegationMetrics: DelegationMetrics | null
  fetchTasks: () => Promise<void>
  fetchDelegationMetrics: () => Promise<void>
  setSelectedTask: (t: OrchestratorTask | null) => void
  setCreateDialogOpen: (v: boolean) => void
  createTask: (input: CreateTaskInput) => Promise<OrchestratorTask>
  cancelTask: (id: string) => Promise<void>
  deleteTask: (id: string) => Promise<void>
}

/** What the create dialog collects: a title, optional details and a model. */
export interface CreateTaskInput {
  name: string
  description?: string
  model?: string
}

/**
 * buildTaskPrompt turns the dialog fields into the task prompt: POST
 * /api/v1/orchestrator/tasks takes a required `prompt` (the task name is
 * derived from it server-side), not name/description.
 */
export function buildTaskPrompt(input: CreateTaskInput): string {
  const name = input.name.trim()
  const description = (input.description ?? '').trim()
  if (!description) return name
  if (!name) return description
  return `${name}\n\n${description}`
}

export const useOrchestratorStore = create<OrchestratorStore>((set, get) => ({
  tasks: [],
  selectedTask: null,
  loading: false,
  createDialogOpen: false,
  delegationMetrics: null,

  fetchTasks: async () => {
    set({ loading: true })
    try {
      const data = await api.get<{ tasks: OrchestratorTask[]; total: number }>('/api/v1/orchestrator/tasks')
      set({ tasks: data.tasks ?? [] })
    } catch {
      set({ tasks: [] })
    } finally {
      set({ loading: false })
    }
  },

  fetchDelegationMetrics: async () => {
    try {
      const data = await api.get<DelegationMetrics>('/api/v1/orchestrator/delegation/metrics')
      set({ delegationMetrics: data })
    } catch {
      set({ delegationMetrics: null })
    }
  },

  setSelectedTask: (selectedTask) => set({ selectedTask }),
  setCreateDialogOpen: (createDialogOpen) => set({ createDialogOpen }),

  createTask: async (input) => {
    const prompt = buildTaskPrompt(input)
    if (!prompt) throw new Error('A task needs a name or a prompt')
    const body: { prompt: string; background: boolean; model?: string } = { prompt, background: true }
    if (input.model) body.model = input.model
    const task = await api.post<OrchestratorTask>('/api/v1/orchestrator/tasks', body)
    await get().fetchTasks()
    return task
  },

  cancelTask: async (id) => {
    try {
      await api.post(`/api/v1/orchestrator/tasks/${id}/cancel`, {})
      await get().fetchTasks()
    } catch (e) {
      console.error('Cancel task failed:', e)
    }
  },

  deleteTask: async (id) => {
    try {
      await api.delete(`/api/v1/orchestrator/tasks/${id}`)
      set((s) => ({ tasks: s.tasks.filter((t) => t.id !== id) }))
    } catch (e) {
      console.error('Delete task failed:', e)
    }
  },
}))
