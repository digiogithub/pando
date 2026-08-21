import api from './api'

/**
 * State of the memory capability: whether remembrances and knowledge-base
 * documents written on this machine are being shipped to a store outside it.
 *
 * The endpoint answers on every build, with `enabled: false` on a standard
 * one, so the UI has a single code path. That matters more than it looks: if
 * "no such feature" and "feature switched off" arrived as different failures,
 * the indicator could end up silent in exactly the case where it must not be.
 */

export interface ExtensionMemorySink {
  id: string
  name: string
  active: boolean
  dryRun: boolean
  destination?: string
  scopes?: string[]
  pending: number
  sent: number
  dropped: number
  lastSyncAt?: string
  lastError?: string
  /** False when the sink ships data but reports no state of its own. */
  reports: boolean
}

export interface ExtensionMemoryHostStats {
  sinks: number
  published: number
  filtered: number
  dropped: number
  failed: number
  queued: number
}

export interface ExtensionMemoryStatus {
  /** The configuration gate. */
  enabled: boolean
  /** The gate is open and something is wired to receive events. */
  active: boolean
  dryRun: boolean
  mode: string
  scopes?: string[]
  paths?: string[]
  origins?: string[]
  wrapSearch: boolean
  wrappers?: string[]
  host: ExtensionMemoryHostStats
  sinks: ExtensionMemorySink[]
}

export async function fetchExtensionMemoryStatus(): Promise<ExtensionMemoryStatus> {
  return api.get<ExtensionMemoryStatus>('/api/v1/extensions/memory')
}
