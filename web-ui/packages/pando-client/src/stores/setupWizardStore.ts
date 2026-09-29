import { create } from 'zustand'
import { api } from '../services/api'

/** Mirrors config.SetupStatus (GET /api/v1/setup/status). */
export interface SetupStatus {
  workingDir: string
  isHomeDir: boolean
  hasLocalConfig: boolean
  localConfigPath?: string
  hasGlobalConfig: boolean
  globalConfigPath?: string
  defaultGlobalConfigPath?: string
  providerAccounts: number
  hasUsableProvider: boolean
  coderModel?: string
  coderModelValid: boolean
  remembrancesEnabled: boolean
  completed: boolean
  needed: boolean
}

interface SetupWizardState {
  status: SetupStatus | null
  open: boolean
  /** Cancelled in this session: do not open by itself again until reload. */
  cancelled: boolean
  /** True when the last attempt to record completion failed. */
  completeError: boolean
  fetchStatus: () => Promise<SetupStatus | null>
  /** Replaces the cached status (e.g. with the one a scope change returned). */
  setStatus: (status: SetupStatus) => void
  /** Opens the assistant on demand (from the config banner or a command). */
  openWizard: () => void
  /** Closes without finishing: the original screens stay as they were. */
  cancel: () => void
  /** Records the assistant as finished and closes it; stays open on failure. */
  complete: () => Promise<boolean>
}

export const useSetupWizardStore = create<SetupWizardState>((set, get) => ({
  status: null,
  open: false,
  cancelled: false,
  completeError: false,

  fetchStatus: async () => {
    try {
      const status = await api.get<SetupStatus>('/api/v1/setup/status')
      set((s) => ({ status, open: s.open || (status.needed && !s.cancelled) }))
      return status
    } catch {
      return get().status
    }
  },

  setStatus: (status) => set({ status }),

  openWizard: () => set({ open: true }),

  cancel: () => set({ open: false, cancelled: true, completeError: false }),

  complete: async () => {
    set({ completeError: false })
    try {
      const status = await api.post<SetupStatus>('/api/v1/setup/complete', {})
      set({ status, open: false })
      return true
    } catch {
      // Keep the wizard open: the completed marker was not written.
      set({ completeError: true })
      return false
    }
  },
}))
