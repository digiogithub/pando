import { describe, expect, it } from 'vitest'
import type { Message, Session } from '../types'
import { applyRawMessage, reconcileMessages, removeMessage, removeSession, upsertSession } from './liveUpdates'
import type { RawMessage, RawSession } from './mappers'

const raw = (id: string, createdAt: number, text: string, extra: Partial<RawMessage> = {}): RawMessage => ({
  ID: id,
  SessionID: 's1',
  Role: 'assistant',
  Parts: [{ text }],
  CreatedAt: createdAt,
  ...extra,
})

const rawSession = (id: string, updatedAt: number, extra: Partial<RawSession> = {}): RawSession => ({
  ID: id,
  Title: id,
  MessageCount: 1,
  PromptTokens: 0,
  CompletionTokens: 0,
  Cost: 0,
  CreatedAt: updatedAt,
  UpdatedAt: updatedAt,
  ...extra,
})

describe('applyRawMessage', () => {
  it('upserts by id in place and keeps untouched messages', () => {
    let msgs: Message[] = []
    msgs = applyRawMessage(msgs, raw('m1', 10, 'hi'))
    msgs = applyRawMessage(msgs, raw('m2', 20, 'he'))
    const first = msgs[0]
    const next = applyRawMessage(msgs, raw('m2', 20, 'hello'))
    expect(next).not.toBe(msgs)
    expect(next[0]).toBe(first)
    expect(next.map((m) => m.id)).toEqual(['m1', 'm2'])
    expect(next[1].content[0].text).toBe('hello')
  })

  it('returns the same array when nothing changed', () => {
    const msgs = applyRawMessage([], raw('m1', 10, 'hi'))
    expect(applyRawMessage(msgs, raw('m1', 10, 'hi'))).toBe(msgs)
  })

  it('inserts an older message in created_at order', () => {
    let msgs = applyRawMessage([], raw('m2', 20, 'b'))
    msgs = applyRawMessage(msgs, raw('m1', 10, 'a'))
    expect(msgs.map((m) => m.id)).toEqual(['m1', 'm2'])
  })

  it('merges tool results into the matching tool call and keeps them on later updates', () => {
    const call = { id: 'c1', name: 'bash', input: '{"cmd":"ls"}' }
    let msgs = applyRawMessage([], raw('m1', 10, '', { Parts: [call] }))
    msgs = applyRawMessage(msgs, raw('t1', 11, '', { Role: 'tool', Parts: [{ tool_call_id: 'c1', content: 'ok', is_error: false }] }))
    expect(msgs).toHaveLength(1)
    expect(msgs[0].content[0]).toMatchObject({ type: 'tool_call', tool_result: 'ok' })
    // A later update of the assistant message must not drop the result.
    msgs = applyRawMessage(msgs, raw('m1', 10, '', { Parts: [call, { text: 'done' }] }))
    expect(msgs[0].content[0]).toMatchObject({ tool_result: 'ok' })
    expect(msgs[0].content[1]).toMatchObject({ type: 'text', text: 'done' })
  })

  it('removes a message by id', () => {
    const msgs = applyRawMessage([], raw('m1', 10, 'hi'))
    expect(removeMessage(msgs, 'm1')).toEqual([])
    expect(removeMessage(msgs, 'nope')).toBe(msgs)
  })
})

describe('reconcileMessages', () => {
  it('keeps identity of unchanged messages', () => {
    const prev = applyRawMessage(applyRawMessage([], raw('m1', 10, 'a')), raw('m2', 20, 'b'))
    const fresh = applyRawMessage(applyRawMessage([], raw('m1', 10, 'a')), raw('m2', 20, 'b2'))
    const out = reconcileMessages(prev, fresh)
    expect(out[0]).toBe(prev[0])
    expect(out[1]).toBe(fresh[1])
    expect(reconcileMessages(prev, applyRawMessage(applyRawMessage([], raw('m1', 10, 'a')), raw('m2', 20, 'b')))).toBe(prev)
  })
})

describe('upsertSession / removeSession', () => {
  it('adds new sessions in updated_at order and moves updated ones', () => {
    let list: Session[] = []
    let r = upsertSession(list, rawSession('a', 10))
    expect(r.added).toBe(true)
    list = r.sessions
    list = upsertSession(list, rawSession('b', 20)).sessions
    expect(list.map((s) => s.id)).toEqual(['b', 'a'])
    r = upsertSession(list, rawSession('a', 30, { Title: 'renamed', busy_elsewhere: true }))
    expect(r.added).toBe(false)
    expect(r.sessions.map((s) => s.id)).toEqual(['a', 'b'])
    expect(r.sessions[0]).toMatchObject({ title: 'renamed', busy_elsewhere: true })
  })

  it('keeps client-only fields and returns the same list when unchanged', () => {
    const list = upsertSession([], rawSession('a', 10)).sessions
    list[0] = { ...list[0], context_window: 1000 }
    const same = upsertSession(list, rawSession('a', 10))
    expect(same.sessions).toBe(list)
    const updated = upsertSession(list, rawSession('a', 11))
    expect(updated.sessions[0].context_window).toBe(1000)
  })

  it('removes sessions', () => {
    const list = upsertSession([], rawSession('a', 10)).sessions
    expect(removeSession(list, 'a')).toEqual({ sessions: [], removed: true })
    expect(removeSession(list, 'x').removed).toBe(false)
  })
})
