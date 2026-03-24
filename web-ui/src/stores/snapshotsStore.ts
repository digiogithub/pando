import { create } from 'zustand'
import type { Snapshot } from '@/types'
import api from '@/services/api'

interface SnapshotsStore {
  snapshots: Snapshot[]
  loading: boolean
  creating: boolean
  createDialogOpen: boolean
  fetchSnapshots: () => Promise<void>
  setCreateDialogOpen: (v: boolean) => void
  createSnapshot: (name: string) => Promise<void>
  applySnapshot: (id: string) => Promise<void>
  revertSnapshot: (id: string) => Promise<void>
  deleteSnapshot: (id: string) => Promise<void>
}

export const useSnapshotsStore = create<SnapshotsStore>((set, get) => ({
  snapshots: [],
  loading: false,
  creating: false,
  createDialogOpen: false,

  fetchSnapshots: async () => {
    set({ loading: true })
    try {
      const snapshots = await api.get<Snapshot[]>('/api/v1/snapshots')
      set({ snapshots: snapshots ?? [] })
    } catch {
      set({ snapshots: [] })
    } finally {
      set({ loading: false })
    }
  },

  setCreateDialogOpen: (createDialogOpen) => set({ createDialogOpen }),

  createSnapshot: async (name: string) => {
    set({ creating: true })
    try {
      await api.post('/api/v1/snapshots', { name })
      await get().fetchSnapshots()
      set({ createDialogOpen: false })
    } finally {
      set({ creating: false })
    }
  },

  applySnapshot: async (id: string) => {
    await api.post(`/api/v1/snapshots/${id}/apply`, {})
    await get().fetchSnapshots()
  },

  revertSnapshot: async (id: string) => {
    await api.post(`/api/v1/snapshots/${id}/revert`, {})
    await get().fetchSnapshots()
  },

  deleteSnapshot: async (id: string) => {
    await api.delete(`/api/v1/snapshots/${id}`)
    set((s) => ({ snapshots: s.snapshots.filter((snap) => snap.id !== id) }))
  },
}))
