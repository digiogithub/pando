import { create } from 'zustand'

/**
 * The persona that persona auto-selection applied to the active session, set
 * from the `Persona: <name> (...)` notice of a turn. The persona selector shows
 * it in its Auto entry; it re-reads GET /api/v1/personas/active for the source.
 */
interface PersonaRoutingStore {
  applied: string | null
  /** bumped on every persona notice so listeners can refetch */
  noticeCount: number
  setApplied: (name: string | null) => void
}

export const usePersonaRoutingStore = create<PersonaRoutingStore>((set) => ({
  applied: null,
  noticeCount: 0,
  setApplied: (name) => set((s) => ({ applied: name, noticeCount: name ? s.noticeCount + 1 : s.noticeCount })),
}))

const PERSONA_NOTICE = /^Persona:\s+([A-Za-z0-9._-]+)/

/** Returns the persona name of a `Persona: <name> (...)` notice, or null. */
export function parsePersonaNotice(text: string): string | null {
  const m = PERSONA_NOTICE.exec(text.trim())
  return m ? m[1] : null
}
