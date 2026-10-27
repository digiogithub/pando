import { create } from 'zustand'
import api from '../services/api'
import type { MesnadaEngineStatus } from '../types'

// Pando's own engines: always enabled, never written to disabledEngines.
export const NATIVE_ENGINES = ['pando', 'warm-acp', 'acp-server'] as const

const ENGINE_ALIASES: Record<string, string> = { 'claude-code': 'claude', 'gemini-cli': 'gemini' }

/** Canonical engine name (aliases resolved, trimmed, lowercased), as the backend stores it. */
export function normalizeEngineName(name: string): string {
  const n = name.trim().toLowerCase()
  return ENGINE_ALIASES[n] ?? n
}

export function isNativeEngine(name: string): boolean {
  const n = normalizeEngineName(name)
  return n === '' || (NATIVE_ENGINES as readonly string[]).includes(n)
}

/**
 * Returns a new disabled-engines list with `name` disabled or re-enabled.
 * Native engines are never added; the result is deduplicated and sorted, which
 * matches the normalisation the backend applies on save.
 */
export function setEngineDisabled(list: string[] | undefined, name: string, disabled: boolean): string[] {
  const target = normalizeEngineName(name)
  const next = new Set((list ?? []).map(normalizeEngineName).filter((n) => n !== '' && !isNativeEngine(n)))
  if (disabled && !isNativeEngine(target)) {
    next.add(target)
  } else {
    next.delete(target)
  }
  return [...next].sort()
}

/**
 * Overlays a (possibly unsaved) disabled list on the detected engines so the
 * switches reflect pending edits. Locked (native) engines always stay enabled.
 */
export function withDisabled(engines: MesnadaEngineStatus[], disabled: string[] | undefined): MesnadaEngineStatus[] {
  const off = new Set((disabled ?? []).map(normalizeEngineName))
  return engines.map((e) => ({ ...e, enabled: e.locked ? true : !off.has(normalizeEngineName(e.name)) }))
}

interface MesnadaEnginesStore {
  engines: MesnadaEngineStatus[]
  loading: boolean
  error: string | null
  fetchEngines: () => Promise<void>
}

// Detected subagent engines for the Mesnada settings page. Enabling/disabling is
// edited through the services config (mesnada.orchestrator.disabledEngines) so
// it is saved with the rest of the page.
export const useMesnadaEnginesStore = create<MesnadaEnginesStore>((set) => ({
  engines: [],
  loading: false,
  error: null,

  fetchEngines: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ engines: MesnadaEngineStatus[] }>('/api/v1/mesnada/engines')
      set({ engines: data.engines ?? [] })
    } catch (e) {
      set({ engines: [], error: e instanceof Error ? e.message : 'Failed to load engines' })
    } finally {
      set({ loading: false })
    }
  },
}))
