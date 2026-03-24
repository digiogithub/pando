import { create } from 'zustand'
import type { Snapshot } from '@/types'
import api from '@/services/api'
import { useToastStore } from './toastStore'

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
      useToastStore.getState().addToast(`Snapshot "${name}" created`, 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to create snapshot',
        'error',
      )
    } finally {
      set({ creating: false })
    }
  },

  applySnapshot: async (id: string) => {
    try {
      await api.post(`/api/v1/snapshots/${id}/apply`, {})
      await get().fetchSnapshots()
      useToastStore.getState().addToast('Snapshot applied', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to apply snapshot',
        'error',
      )
    }
  },

  revertSnapshot: async (id: string) => {
    try {
      await api.post(`/api/v1/snapshots/${id}/revert`, {})
      await get().fetchSnapshots()
      useToastStore.getState().addToast('Snapshot reverted', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to revert snapshot',
        'error',
      )
    }
  },

  deleteSnapshot: async (id: string) => {
    try {
      await api.delete(`/api/v1/snapshots/${id}`)
      set((s) => ({ snapshots: s.snapshots.filter((snap) => snap.id !== id) }))
      useToastStore.getState().addToast('Snapshot deleted', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to delete snapshot',
        'error',
      )
    }
  },
}))
