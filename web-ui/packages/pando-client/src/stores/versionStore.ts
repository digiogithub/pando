import { create } from 'zustand'
import api from '../services/api'

/** Running Pando version and update availability, from GET /api/v1/version. */
export interface VersionStatus {
  /** Running build, with a leading "v" when it is a semantic version. */
  version: string
  /** Newest release for this platform, when known. */
  latest?: string
  /** True when `latest` is newer than `version`. */
  update_available: boolean
  /** CLI command that installs the update (e.g. "pando update"). */
  update_command?: string
  /** Release notes URL of `latest`. */
  release_url?: string
  /** False for development builds that cannot be compared with releases. */
  checkable: boolean
}

/** Retry delays after a failed load (e.g. a 401 before the session token is set). */
const RETRY_DELAYS_MS = [2_000, 5_000, 15_000]

interface VersionState {
  status: VersionStatus | null
  loading: boolean
  /** Loads the status once; later calls are no-ops unless `force` is set. */
  fetchVersion: (force?: boolean) => Promise<void>
}

export const useVersionStore = create<VersionState>((set, get) => ({
  status: null,
  loading: false,

  fetchVersion: async (force = false) => {
    if (get().loading || (get().status && !force)) return
    set({ loading: true })
    for (let attempt = 0; ; attempt++) {
      try {
        const status = await api.get<VersionStatus>('/api/v1/version')
        set({ status, loading: false })
        return
      } catch {
        if (attempt >= RETRY_DELAYS_MS.length) {
          set({ loading: false })
          return
        }
        await new Promise((resolve) => setTimeout(resolve, RETRY_DELAYS_MS[attempt]))
      }
    }
  },
}))
