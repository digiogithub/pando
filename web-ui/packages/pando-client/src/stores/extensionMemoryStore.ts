import { create } from 'zustand'
import {
  fetchExtensionMemoryStatus,
  type ExtensionMemoryStatus,
} from '../services/extensionMemory'

interface ExtensionMemoryState {
  status: ExtensionMemoryStatus | null
  loaded: boolean
  load: () => Promise<void>
  refresh: () => Promise<void>
}

/**
 * Holds the memory-capability status behind the sync indicator.
 *
 * Unlike the panel manifest this is *not* fixed for the life of the process —
 * counters move and a sink can start failing — so it is refreshable. A failed
 * request leaves the previous status in place rather than clearing it: an
 * indicator that blinks off because one poll failed would read as "sync
 * stopped", which is the one thing it must never say by accident.
 */
export const useExtensionMemoryStore = create<ExtensionMemoryState>((set, get) => ({
  status: null,
  loaded: false,

  load: async () => {
    if (get().loaded) return
    await get().refresh()
  },

  refresh: async () => {
    try {
      const status = await fetchExtensionMemoryStatus()
      set({ status, loaded: true })
    } catch {
      set({ loaded: true })
    }
  },
}))
