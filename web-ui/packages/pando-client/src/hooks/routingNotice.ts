import { useSessionStore } from '../stores/sessionStore'
import { useModelAutoModeStore } from '../stores/modelAutoModeStore'
import { usePersonaRoutingStore, parsePersonaNotice } from '../stores/personaRoutingStore'
import type { Message, SSEEvent } from '../types'

/**
 * Handle a `system_message` SSE event: a model auto mode routing notice (with a
 * structured `routing` payload) or a generic system notice. Both become a
 * muted row inserted before the streaming assistant bubble; a routing notice
 * also updates the model shown as `Auto · <model>` in the model switcher.
 */
export function handleSystemMessageEvent(event: SSEEvent, fallbackSessionId: string): void {
  const text = (event.message ?? '').trim()
  const routing = event.routing
  if (!text && !routing) return

  if (routing?.model) {
    useModelAutoModeStore.getState().setLastRoutedModel(routing.model)
  }

  const appliedPersona = !routing ? parsePersonaNotice(text) : null
  if (appliedPersona) {
    usePersonaRoutingStore.getState().setApplied(appliedPersona)
  }

  const msg: Message = {
    id: `notice-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    session_id: event.session_id ?? fallbackSessionId,
    role: 'system',
    content: [{ type: 'text', text: text || routing?.notice || '' }],
    created_at: new Date().toISOString(),
    routing,
    notice: !routing,
  }
  useSessionStore.getState().insertBeforeLast(msg)
}

/** i18n keys (web-ui locales, `chat.notice.*`) of the delegation notices. */
const DELEGATION_NOTICE_KEYS: Partial<Record<SSEEvent['type'], string>> = {
  resurrected: 'chat.notice.resurrected',
  conclusion_queued: 'chat.notice.conclusionQueued',
  conclusion_injected: 'chat.notice.conclusionInjected',
}

/**
 * Handle the delegation events (`resurrected`, `conclusion_queued`,
 * `conclusion_injected`): a muted row before the streaming assistant bubble.
 * `resurrected` is what frames a run the server started on its own after a
 * delegated task reported its result. The row stores an i18n key, not text, so it
 * is translated where it is rendered. Returns false for any other event.
 */
export function handleDelegationEvent(event: SSEEvent, fallbackSessionId: string): boolean {
  const noticeKey = DELEGATION_NOTICE_KEYS[event.type]
  if (!noticeKey) return false

  // A reattach replays the run from its first event: do not stack the same
  // marker twice in front of the bubble being streamed into.
  const msgs = useSessionStore.getState().messages
  const before: Message | undefined = msgs[msgs.length - 2]
  if (event.type === 'resurrected' && before?.notice && before.noticeKey === noticeKey) return true

  useSessionStore.getState().insertBeforeLast({
    id: `notice-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    session_id: event.session_id ?? fallbackSessionId,
    role: 'system',
    content: [{ type: 'text', text: event.message ?? '' }],
    created_at: new Date().toISOString(),
    notice: true,
    noticeKey,
  })
  return true
}
