import { useEffect, useRef } from 'react'
import { create } from 'zustand'

/**
 * Registry of settings panels that hold unsaved edits.
 *
 * Each panel with a Save/Reset pattern registers itself through
 * `useUnsavedChangesGuard` with its own dirty flag and its own save/discard
 * functions. SettingsView reads the registry before any transition that would
 * lose those edits (category switch, mobile back, Esc, route change, reload)
 * and asks the user whether to save, discard or stay.
 */

export interface UnsavedEntry {
  /** Registration id; for core panels it is the settings category id. */
  id: string
  dirty: boolean
  /** Optional human-readable section name (used for extension sections). */
  label?: string
  /** Persists the edits. Resolves `false` (or rejects) when the save failed. */
  save: () => Promise<boolean | void>
  /** Drops the edits, restoring the last saved values. */
  discard: () => void
}

interface UnsavedChangesState {
  entries: Record<string, UnsavedEntry>
  register: (entry: UnsavedEntry) => void
  unregister: (id: string) => void
  /** True when any registered panel has unsaved edits. */
  hasUnsaved: () => boolean
  /** The registrations that currently hold unsaved edits. */
  dirtyEntries: () => UnsavedEntry[]
  /** Saves every dirty panel; resolves true only when all saves succeeded. */
  saveAll: () => Promise<boolean>
  /** Discards the edits of every dirty panel. */
  discardAll: () => void
}

export const useUnsavedChangesStore = create<UnsavedChangesState>((set, get) => ({
  entries: {},

  register: (entry) => set((s) => ({ entries: { ...s.entries, [entry.id]: entry } })),

  unregister: (id) =>
    set((s) => {
      if (!(id in s.entries)) return s
      const entries = { ...s.entries }
      delete entries[id]
      return { entries }
    }),

  hasUnsaved: () => Object.values(get().entries).some((e) => e.dirty),

  dirtyEntries: () => Object.values(get().entries).filter((e) => e.dirty),

  saveAll: async () => {
    let ok = true
    for (const entry of get().dirtyEntries()) {
      try {
        const result = await entry.save()
        if (result === false) ok = false
      } catch {
        ok = false
      }
    }
    return ok
  },

  discardAll: () => {
    for (const entry of get().dirtyEntries()) entry.discard()
  },
}))

export interface UnsavedChangesGuardOptions {
  id: string
  dirty: boolean
  save: () => Promise<boolean | void>
  discard: () => void
  label?: string
}

/**
 * Registers the calling panel in the unsaved-changes registry for as long as it
 * is mounted. `save` and `discard` may be fresh closures on every render: the
 * registry always calls the latest ones.
 */
export function useUnsavedChangesGuard({ id, dirty, save, discard, label }: UnsavedChangesGuardOptions) {
  const saveRef = useRef(save)
  const discardRef = useRef(discard)

  useEffect(() => {
    saveRef.current = save
    discardRef.current = discard
  }, [save, discard])

  useEffect(() => {
    useUnsavedChangesStore.getState().register({
      id,
      dirty,
      label,
      save: () => saveRef.current(),
      discard: () => discardRef.current(),
    })
  }, [id, dirty, label])

  useEffect(() => {
    return () => useUnsavedChangesStore.getState().unregister(id)
  }, [id])
}
