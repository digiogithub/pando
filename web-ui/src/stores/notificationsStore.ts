import { create } from 'zustand'
import { getBaseURL } from '@/services/api'
import api from '@/services/api'
import { useToastStore } from './toastStore'

export type NotificationLevel = 'info' | 'warn' | 'error'
export type NotificationSource =
  | 'llm_provider'
  | 'tool'
  | 'lsp'
  | 'agent'
  | 'system'

export interface Notification {
  id: string
  time: string
  level: NotificationLevel
  source: NotificationSource
  message: string
  /** TTL in nanoseconds (0 = permanent) */
  ttl: number
}

interface NotificationsStore {
  connected: boolean
  _es: EventSource | null
  connect: () => void
  disconnect: () => void
}

let _backoffMs = 1_000
const MAX_BACKOFF_MS = 30_000

/** Map backend level → toast type */
function toToastType(level: NotificationLevel): 'info' | 'warning' | 'error' {
  if (level === 'warn') return 'warning'
  return level // 'info' | 'error'
}

export const useNotificationsStore = create<NotificationsStore>((set, get) => ({
  connected: false,
  _es: null,

  connect: () => {
    if (get()._es) return

    const open = () => {
      const token = api.getToken()
      const base = getBaseURL()
      const url = token
        ? `${base}/api/v1/notifications/stream?token=${encodeURIComponent(token)}`
        : `${base}/api/v1/notifications/stream`

      const es = new EventSource(url)

      es.addEventListener('connected', () => {
        _backoffMs = 1_000
        set({ connected: true, _es: es })
      })

      es.addEventListener('notification', (evt: MessageEvent) => {
        try {
          const n: Notification = JSON.parse(evt.data as string)
          const { addToast } = useToastStore.getState()

          // Compute auto-dismiss delay from TTL (nanoseconds → ms).
          // TTL=0 means permanent; we default to 8 s in that case.
          const ttlMs = n.ttl > 0 ? Math.round(n.ttl / 1_000_000) : 8_000

          addToast(n.message, toToastType(n.level), ttlMs)
        } catch {
          // ignore malformed events
        }
      })

      es.onerror = () => {
        es.close()
        set({ connected: false, _es: null })
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
