import { useSessionStore } from '../stores/sessionStore'
import { useModelAutoModeStore } from '../stores/modelAutoModeStore'
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
