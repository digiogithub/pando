import { create } from 'zustand'
import { openEventSource, parseEventData } from '../services/eventSource'
import type { RawSession } from '../services/mappers'
import { useSessionStore } from './sessionStore'

const MIN_BACKOFF_MS = 1_000
const MAX_BACKOFF_MS = 30_000

interface SessionEventsStore {
  connected: boolean
  /** subscribe to /api/v1/sessions/events (once, app level) */
  connect: () => void
  disconnect: () => void
}

let es: EventSource | null = null
let retryTimer: ReturnType<typeof setTimeout> | undefined
let backoffMs = MIN_BACKOFF_MS
// true after a dropped connection: the list may have missed events, so the
// next `ready` refetches it once.
let resyncOnReady = false

/**
 * Pushes session list changes into sessionStore: sessions created, renamed,
 * finished or run by another process (subagents, busy_elsewhere) appear and
 * update in the sidebar without polling. Reconnects with exponential backoff.
 */
export const useSessionEventsStore = create<SessionEventsStore>((set) => ({
  connected: false,

  connect: () => {
    if (es || retryTimer !== undefined) return
    const open = () => {
      retryTimer = undefined
      const source = openEventSource('/api/v1/sessions/events')
      es = source

      source.addEventListener('ready', () => {
        backoffMs = MIN_BACKOFF_MS
        set({ connected: true })
        if (resyncOnReady) {
          resyncOnReady = false
          void useSessionStore.getState().fetchSessions()
        }
      })
      source.addEventListener('session_upsert', (evt: MessageEvent) => {
        const data = parseEventData<{ session?: RawSession }>(evt)
        if (data?.session) useSessionStore.getState().upsertPushedSession(data.session)
      })
      source.addEventListener('session_deleted', (evt: MessageEvent) => {
        const data = parseEventData<{ id?: string }>(evt)
        if (data?.id) useSessionStore.getState().removePushedSession(data.id)
      })
      source.onerror = () => {
        source.close()
        if (es !== source) return
        es = null
        resyncOnReady = true
        set({ connected: false })
        retryTimer = setTimeout(open, backoffMs)
        backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF_MS)
      }
    }
    open()
  },

  disconnect: () => {
    if (retryTimer !== undefined) clearTimeout(retryTimer)
    retryTimer = undefined
    es?.close()
    es = null
    set({ connected: false })
  },
}))
