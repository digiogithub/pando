// In-place updates of the loaded session list and transcript from server push
// events (/api/v1/sessions/events and /api/v1/sessions/{id}/watch). Every
// helper returns the SAME array when nothing changed and keeps the object
// identity of untouched entries, so React re-renders only what changed and the
// message list is never cleared, refilled or remounted (no flicker, scroll
// position kept).
import type { Message, Session } from '../types'
import { mapMessageWithResults, mapSession, type RawMessage, type RawSession } from './mappers'

function sameJSON(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

/** tool results already merged into a message's tool_call parts, by call id */
function knownResults(msg: Message | undefined): Map<string, { content: string; is_error: boolean }> {
  const out = new Map<string, { content: string; is_error: boolean }>()
  for (const part of msg?.content ?? []) {
    if (part.type === 'tool_call' && part.tool_call_id && part.tool_result !== undefined) {
      out.set(part.tool_call_id, { content: part.tool_result, is_error: part.is_error ?? false })
    }
  }
  return out
}

/**
 * Applies one raw message (the Go message.Message JSON, as the history API
 * returns it) to the transcript: a role="tool" message fills the results of
 * the matching tool_call parts, any other message is upserted by id (inserted
 * in created_at order when new).
 */
export function applyRawMessage(messages: Message[], raw: RawMessage): Message[] {
  if (raw.Role === 'tool') {
    let changed = false
    const results = new Map<string, { content: string; is_error: boolean }>()
    for (const p of raw.Parts ?? []) {
      if (p.tool_call_id && p.content !== undefined) {
        results.set(p.tool_call_id, { content: p.content, is_error: p.is_error ?? false })
      }
    }
    const next = messages.map((msg) => {
      if (!msg.content.some((c) => c.type === 'tool_call' && c.tool_call_id && results.has(c.tool_call_id))) return msg
      const content = msg.content.map((c) => {
        const r = c.type === 'tool_call' && c.tool_call_id ? results.get(c.tool_call_id) : undefined
        if (!r || (c.tool_result === r.content && Boolean(c.is_error) === r.is_error)) return c
        changed = true
        return { ...c, tool_result: r.content, is_error: r.is_error }
      })
      return changed ? { ...msg, content } : msg
    })
    return changed ? next : messages
  }

  const idx = messages.findIndex((m) => m.id === raw.ID)
  const mapped = mapMessageWithResults(raw, knownResults(idx >= 0 ? messages[idx] : undefined))
  if (idx >= 0) {
    if (sameJSON(messages[idx], mapped)) return messages
    const next = messages.slice()
    next[idx] = mapped
    return next
  }
  // New message: usually the newest, otherwise keep created_at order.
  const at = messages.findIndex((m) => m.created_at > mapped.created_at)
  if (at < 0) return [...messages, mapped]
  const next = messages.slice()
  next.splice(at, 0, mapped)
  return next
}

/** Removes a message by id. */
export function removeMessage(messages: Message[], id: string): Message[] {
  return messages.some((m) => m.id === id) ? messages.filter((m) => m.id !== id) : messages
}

/**
 * Takes a freshly loaded transcript but reuses the previous object of every
 * message whose content did not change, so a resync does not re-render (or
 * visually reset) the whole list.
 */
export function reconcileMessages(prev: Message[], next: Message[]): Message[] {
  const byId = new Map(prev.map((m) => [m.id, m]))
  let same = prev.length === next.length
  const out = next.map((m, i) => {
    const old = byId.get(m.id)
    const keep = old && sameJSON(old, m) ? old : m
    if (keep !== prev[i]) same = false
    return keep
  })
  return same ? prev : out
}

/** Session list order: newest activity first, like ListSessions. */
function compareSessions(a: Session, b: Session): number {
  if (a.updated_at !== b.updated_at) return a.updated_at < b.updated_at ? 1 : -1
  if (a.created_at !== b.created_at) return a.created_at < b.created_at ? 1 : -1
  return 0
}

/**
 * Upserts a pushed session list item. Client-only fields (live context window,
 * token estimates) of a known session are kept. Returns whether it was new.
 */
export function upsertSession(sessions: Session[], raw: RawSession): { sessions: Session[]; added: boolean } {
  const mapped = mapSession(raw)
  const idx = sessions.findIndex((s) => s.id === mapped.id)
  const merged = idx >= 0 ? { ...sessions[idx], ...mapped } : mapped
  if (idx >= 0 && sameJSON(sessions[idx], merged)) return { sessions, added: false }
  const next = idx >= 0 ? sessions.map((s, i) => (i === idx ? merged : s)) : [...sessions, merged]
  // Stable sort: only the moved session changes position.
  next.sort(compareSessions)
  return { sessions: next, added: idx < 0 }
}

/** Removes a session by id. */
export function removeSession(sessions: Session[], id: string): { sessions: Session[]; removed: boolean } {
  return sessions.some((s) => s.id === id)
    ? { sessions: sessions.filter((s) => s.id !== id), removed: true }
    : { sessions, removed: false }
}
