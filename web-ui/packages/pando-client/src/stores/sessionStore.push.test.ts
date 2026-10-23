import { beforeEach, describe, expect, it } from 'vitest'
import type { RawMessage, RawSession } from '../services/mappers'
import { useSessionStore } from './sessionStore'

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

const rawMessage = (id: string, text: string): RawMessage => ({
  ID: id,
  SessionID: 's1',
  Role: 'assistant',
  Parts: [{ text }],
  CreatedAt: 10,
})

describe('sessionStore push updates', () => {
  beforeEach(() => {
    useSessionStore.setState({ sessions: [], sessionsTotal: 0, activeSessionId: 's1', messages: [] })
  })

  it('upserts and removes pushed sessions keeping the total', () => {
    const store = useSessionStore.getState()
    store.upsertPushedSession(rawSession('old', 10))
    store.upsertPushedSession(rawSession('subagent', 20, { busy_elsewhere: true }))
    let state = useSessionStore.getState()
    expect(state.sessions.map((s) => s.id)).toEqual(['subagent', 'old'])
    expect(state.sessionsTotal).toBe(2)
    expect(state.sessions[0].busy_elsewhere).toBe(true)

    store.upsertPushedSession(rawSession('subagent', 21, { busy_elsewhere: false }))
    state = useSessionStore.getState()
    expect(state.sessionsTotal).toBe(2)
    expect(state.sessions[0].busy_elsewhere).toBe(false)

    store.removePushedSession('old')
    state = useSessionStore.getState()
    expect(state.sessions.map((s) => s.id)).toEqual(['subagent'])
    expect(state.sessionsTotal).toBe(1)
  })

  it('applies pushed messages in place for the active session only', () => {
    const store = useSessionStore.getState()
    store.applyPushedMessage('s1', rawMessage('m1', 'he'))
    const before = useSessionStore.getState().messages
    store.applyPushedMessage('s1', rawMessage('m1', 'hello'))
    const after = useSessionStore.getState().messages
    expect(after).toHaveLength(1)
    expect(after[0].content[0].text).toBe('hello')
    expect(after).not.toBe(before)

    store.applyPushedMessage('other', rawMessage('x', 'nope'))
    expect(useSessionStore.getState().messages).toBe(after)

    store.removePushedMessage('s1', 'm1')
    expect(useSessionStore.getState().messages).toEqual([])
  })
})
