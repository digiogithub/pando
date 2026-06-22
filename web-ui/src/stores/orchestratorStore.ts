import { create } from 'zustand'
import type { OrchestratorTask, DelegationMetrics } from '@/types'
import api from '@/services/api'

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
  cancelTask: (id: string) => Promise<void>
  deleteTask: (id: string) => Promise<void>
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
