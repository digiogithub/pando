import { useEffect, useState } from 'react'
import { openEventSource, parseEventData } from '@pando/client/services/eventSource'
import type { RawMessage, RawSession } from '@pando/client/services/mappers'
import { useSessionStore } from '@pando/client/stores/sessionStore'

/** delay before reopening a /watch stream that dropped while still busy */
const RECONNECT_DELAY_MS = 2000

/**
 * Follows a session that another Pando process (an IPC follower, typically a
 * mesnada subagent) is running. That process writes the shared database and
 * reports its changes to this server, which pushes them on
 * /api/v1/sessions/{id}/watch as data (`message`, `message_deleted`,
 * `session_update`, `done`). They are applied in place: messages are upserted
 * by id, never cleared and refilled, so the list does not flicker and keeps its
 * scroll position. The history is merged once on open (catch-up) and once on
 * `done`; both merges keep unchanged messages as they are.
 */
export function useExternalSessionWatch(sessionId: string | null, busyElsewhere: boolean, streaming: boolean) {
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!sessionId || !busyElsewhere || streaming) return
    const store = useSessionStore.getState
    let retry: ReturnType<typeof setTimeout> | undefined
    let finished = false

    void store().resyncActiveSession(sessionId)
    const source = openEventSource(`/api/v1/sessions/${encodeURIComponent(sessionId)}/watch`)
    source.addEventListener('message', (evt: MessageEvent) => {
      const data = parseEventData<{ message?: RawMessage }>(evt)
      if (data?.message) store().applyPushedMessage(sessionId, data.message)
    })
    source.addEventListener('message_deleted', (evt: MessageEvent) => {
      const data = parseEventData<{ id?: string }>(evt)
      if (data?.id) store().removePushedMessage(sessionId, data.id)
    })
    source.addEventListener('session_update', (evt: MessageEvent) => {
      const data = parseEventData<{ session?: RawSession }>(evt)
      if (data?.session) store().upsertPushedSession(data.session)
    })
    source.addEventListener('done', () => {
      finished = true
      source.close()
      // The run ended: merge the final history (it also clears busy_elsewhere,
      // which tears this effect down). If the server still reports the session
      // busy, reopen after a pause.
      void store().resyncActiveSession(sessionId)
      retry = setTimeout(() => setAttempt((n) => n + 1), RECONNECT_DELAY_MS)
    })
    source.onerror = () => {
      if (finished) return
      // EventSource would reconnect by itself without a catch-up; do it here.
      source.close()
      retry = setTimeout(() => setAttempt((n) => n + 1), RECONNECT_DELAY_MS)
    }
    return () => {
      source.close()
      if (retry !== undefined) clearTimeout(retry)
    }
  }, [sessionId, busyElsewhere, streaming, attempt])
}
