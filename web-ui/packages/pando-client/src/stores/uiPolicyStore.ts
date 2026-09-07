import { create } from 'zustand'
import { fetchUIPolicy, pathsOverlap, EMPTY_UI_POLICY, type UIPolicy } from '../services/uiPolicy'

interface UIPolicyState {
  policy: UIPolicy
  loaded: boolean
  load: () => Promise<void>
  isHidden: (path: string) => boolean
  isReadOnly: (path: string) => boolean
}

/**
 * Holds the settings UI policy declared by the loaded extensions.
 *
 * Unlike the extension manifest, the policy can change while the app runs (an
 * enrolment completing, a session ending), so `load` re-fetches every time it
 * is called and the settings view calls it when it mounts. A failure is
 * swallowed into the empty policy: the backend refuses the restricted writes
 * regardless, so the worst case is a section offered that the save then
 * declines, never a section silently made editable.
 */
export const useUIPolicyStore = create<UIPolicyState>((set, get) => ({
  policy: EMPTY_UI_POLICY,
  loaded: false,

  load: async () => {
    try {
      set({ policy: await fetchUIPolicy(), loaded: true })
    } catch {
      set({ policy: EMPTY_UI_POLICY, loaded: true })
    }
  },

  isHidden: (path) => get().policy.hiddenSections.some((p) => pathsOverlap(p, path)),

  // A hidden path is not reported as read-only: it is not rendered at all, and
  // the stronger statement wins. This mirrors UIPolicy.IsReadOnly in the host.
  isReadOnly: (path) =>
    !get().isHidden(path) && get().policy.readOnlySections.some((p) => pathsOverlap(p, path)),
}))
