import { create } from 'zustand'
import { fetchExtensionPanels, type ExtensionPanel, type ExtensionSlotName } from '../services/extensionUI'

interface ExtensionPanelsState {
  panels: ExtensionPanel[]
  loaded: boolean
  load: () => Promise<void>
  bySlot: (slot: ExtensionSlotName) => ExtensionPanel[]
}

/**
 * Holds the extension UI manifest.
 *
 * The manifest is fixed for the life of the binary, so it is fetched once and
 * a failure is swallowed into an empty list: extension panels are additive,
 * and a build with none is the normal case. Nothing here may be able to stop
 * the shell from rendering.
 */
export const useExtensionPanelsStore = create<ExtensionPanelsState>((set, get) => ({
  panels: [],
  loaded: false,

  load: async () => {
    if (get().loaded) return
    try {
      const panels = await fetchExtensionPanels()
      set({ panels, loaded: true })
    } catch {
      set({ panels: [], loaded: true })
    }
  },

  bySlot: (slot) => get().panels.filter((p) => p.slot === slot),
}))
