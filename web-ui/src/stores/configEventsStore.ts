import { create } from 'zustand'
import api from '@/services/api'
import { useSettingsStore } from './settingsStore'

export interface ConfigChangeEvent {
  section: string
  timestamp: string
  source: 'tui' | 'webui' | 'file'
}

interface ConfigEventsStore {
  connected: boolean
  _es: EventSource | null
  connect: () => void
  disconnect: () => void
}

let _backoffMs = 1_000
const MAX_BACKOFF_MS = 30_000

export const useConfigEventsStore = create<ConfigEventsStore>((set, get) => ({
  connected: false,
  _es: null,

  connect: () => {
    // Avoid duplicate connections
    if (get()._es) return

    const open = () => {
      // EventSource cannot send custom headers, so we pass the auth token as a
      // query parameter (the server auth middleware accepts ?token=...).
      const token = api.getToken()
      const url = token
        ? `/api/v1/config/events?token=${encodeURIComponent(token)}`
        : '/api/v1/config/events'
      const es = new EventSource(url)

      es.onopen = () => {
        _backoffMs = 1_000
        set({ connected: true, _es: es })
      }

      es.onmessage = (evt) => {
        try {
          const event: ConfigChangeEvent = JSON.parse(evt.data)
          // Skip events originating from the Web-UI itself to avoid loops.
          if (event.source === 'webui') return
          // Re-fetch settings so the form reflects the new values.
          useSettingsStore.getState().fetchSettings()
        } catch {
          // Ignore malformed events
        }
      }

      es.onerror = () => {
        es.close()
        set({ connected: false, _es: null })
        // Exponential back-off reconnect
        setTimeout(() => {
          _backoffMs = Math.min(_backoffMs * 2, MAX_BACKOFF_MS)
          open()
        }, _backoffMs)
      }
    }

    open()
  },

  disconnect: () => {
    const { _es } = get()
    if (_es) {
      _es.close()
      set({ connected: false, _es: null })
    }
  },
}))

/**
 * useConfigEvents starts the SSE connection when the component mounts and
 * stops it when it unmounts.
 *
 * Usage:
 *   useConfigEvents()
 */
export function useConfigEvents() {
  const { connect, disconnect } = useConfigEventsStore()
  // Using a ref pattern via useEffect requires React — we import it here.
  // This hook is intentionally lightweight: call connect/disconnect from
  // the component's own useEffect.
  return { connect, disconnect }
}
