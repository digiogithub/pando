import { create } from 'zustand'
import type { CronJob, CronJobCreate } from '@/types'
import api from '@/services/api'
import { useToastStore } from './toastStore'

interface CronJobsStore {
  jobs: CronJob[]
  loading: boolean
  error: string | null
  fetchJobs: () => Promise<void>
  runJob: (name: string) => Promise<string | null>
  toggleEnabled: (name: string) => Promise<void>
  createJob: (job: CronJobCreate) => Promise<void>
  updateJob: (name: string, fields: Partial<Omit<CronJob, 'name' | 'nextRun'>>) => Promise<void>
  deleteJob: (name: string) => Promise<void>
}

export const useCronJobsStore = create<CronJobsStore>((set, get) => ({
  jobs: [],
  loading: false,
  error: null,

  fetchJobs: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ jobs: CronJob[]; enabled: boolean }>('/api/v1/cronjobs')
      set({ jobs: data.jobs ?? [] })
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to fetch cronjobs'
      set({ error: msg, jobs: [] })
    } finally {
      set({ loading: false })
    }
  },

  runJob: async (name: string) => {
    try {
      const data = await api.post<{ taskId: string }>(`/api/v1/cronjobs/${encodeURIComponent(name)}/run`, {})
      useToastStore.getState().addToast(`CronJob "${name}" triggered`, 'success')
      return data.taskId ?? null
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : `Failed to run cronjob "${name}"`,
        'error',
      )
      return null
    }
  },

  toggleEnabled: async (name: string) => {
    const job = get().jobs.find((j) => j.name === name)
    if (!job) return
    const newEnabled = !job.enabled
    // Optimistic update
    set((s) => ({ jobs: s.jobs.map((j) => j.name === name ? { ...j, enabled: newEnabled } : j) }))
    try {
      await api.put(`/api/v1/cronjobs/${encodeURIComponent(name)}`, { enabled: newEnabled })
      useToastStore.getState().addToast(
        `CronJob "${name}" ${newEnabled ? 'enabled' : 'disabled'}`,
        'success',
      )
    } catch (e) {
      // Revert on failure
      set((s) => ({ jobs: s.jobs.map((j) => j.name === name ? { ...j, enabled: !newEnabled } : j) }))
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : `Failed to update cronjob "${name}"`,
        'error',
      )
    }
  },

  createJob: async (job: CronJobCreate) => {
    try {
      await api.post('/api/v1/cronjobs', job)
      await get().fetchJobs()
      useToastStore.getState().addToast(`CronJob "${job.name}" created`, 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to create cronjob',
        'error',
      )
      throw e
    }
  },

  updateJob: async (name: string, fields: Partial<Omit<CronJob, 'name' | 'nextRun'>>) => {
    try {
      await api.put(`/api/v1/cronjobs/${encodeURIComponent(name)}`, fields)
      await get().fetchJobs()
      useToastStore.getState().addToast(`CronJob "${name}" updated`, 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : `Failed to update cronjob "${name}"`,
        'error',
      )
      throw e
    }
  },

  deleteJob: async (name: string) => {
    try {
      await api.delete(`/api/v1/cronjobs/${encodeURIComponent(name)}`)
      set((s) => ({ jobs: s.jobs.filter((j) => j.name !== name) }))
      useToastStore.getState().addToast(`CronJob "${name}" deleted`, 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : `Failed to delete cronjob "${name}"`,
        'error',
      )
    }
  },
}))
