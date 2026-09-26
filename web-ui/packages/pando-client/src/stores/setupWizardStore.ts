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
  fetchStatus: () => Promise<SetupStatus | null>
  /** Opens the assistant on demand (from the config banner or a command). */
  openWizard: () => void
  /** Closes without finishing: the original screens stay as they were. */
  cancel: () => void
  /** Records the assistant as finished and closes it. */
  complete: () => Promise<void>
}

export const useSetupWizardStore = create<SetupWizardState>((set, get) => ({
  status: null,
  open: false,
  cancelled: false,

  fetchStatus: async () => {
    try {
      const status = await api.get<SetupStatus>('/api/v1/setup/status')
      set((s) => ({ status, open: s.open || (status.needed && !s.cancelled) }))
      return status
    } catch {
      return get().status
    }
  },

  openWizard: () => set({ open: true }),

  cancel: () => set({ open: false, cancelled: true }),

  complete: async () => {
    try {
      const status = await api.post<SetupStatus>('/api/v1/setup/complete', {})
      set({ status, open: false })
    } catch {
      set({ open: false })
    }
  },
}))
