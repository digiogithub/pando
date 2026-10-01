import { beforeEach, describe, expect, it } from 'vitest'
import { RUN_SEQ_STALE, shouldReattach } from '@pando/client/services/runSeq'
import { mapSession } from '@pando/client/services/mappers'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import api from '@pando/client/services/api'

describe('shouldReattach', () => {
  it('reattaches to a newer run even when it already finished', () => {
    expect(shouldReattach({ serverSeq: 3, seenSeq: 2, streaming: false })).toBe(true)
  })
  it('does not replay the run the client already streamed', () => {
    expect(shouldReattach({ serverSeq: 2, seenSeq: 2, streaming: false })).toBe(false)
    expect(shouldReattach({ serverSeq: 1, seenSeq: 2, streaming: false })).toBe(false)
  })
  it('stays out of the way while a stream is open', () => {
    expect(shouldReattach({ serverSeq: 3, seenSeq: 2, streaming: true })).toBe(false)
  })
  it('waits for the history baseline before deciding', () => {
    expect(shouldReattach({ serverSeq: 3, seenSeq: undefined, streaming: false })).toBe(false)
    expect(shouldReattach({ serverSeq: undefined, seenSeq: 2, streaming: false })).toBe(false)
  })
  it('a stale baseline reattaches on any server state, including run 0', () => {
    expect(shouldReattach({ serverSeq: 0, seenSeq: RUN_SEQ_STALE, streaming: false })).toBe(true)
  })
})

describe('run_seq in the store', () => {
  beforeEach(() => {
    useSessionStore.setState({ sessions: [], activeSessionId: null, seenRunSeq: {}, messages: [] })
  })

  it('maps run_seq and is_running from the session list', () => {
    const s = mapSession({ ID: 'a', Title: 't', MessageCount: 0, PromptTokens: 0, CompletionTokens: 0, Cost: 0, CreatedAt: 0, UpdatedAt: 0, is_running: true, run_seq: 4 })
    expect(s.run_seq).toBe(4)
    expect(s.is_running).toBe(true)
  })

  it('selecting an idle session marks its sequence as seen', async () => {
    const orig = api.get
    api.get = (async (url: string) => {
      if (url.endsWith('/auto-approve')) return { enabled: false }
      return { session: {}, messages: [], is_running: false, run_seq: 5 }
    }) as typeof api.get
    try {
      await useSessionStore.getState().setActiveSession('s1')
    } finally {
      api.get = orig
    }
    expect(useSessionStore.getState().seenRunSeq.s1).toBe(5)
  })

  it('selecting a running session leaves the baseline stale so it reattaches', async () => {
    const orig = api.get
    api.get = (async (url: string) => {
      if (url.endsWith('/auto-approve')) return { enabled: false }
      return { session: {}, messages: [], is_running: true, run_seq: 5 }
    }) as typeof api.get
    try {
      await useSessionStore.getState().setActiveSession('s1')
    } finally {
      api.get = orig
    }
    expect(useSessionStore.getState().seenRunSeq.s1).toBe(RUN_SEQ_STALE)
  })

  it('the pending poll records a newer run_seq on the session', async () => {
    useSessionStore.setState({
      activeSessionId: 's1',
      sessions: [{ id: 's1', title: '', message_count: 0, prompt_tokens: 0, completion_tokens: 0, cost: 0, created_at: '', updated_at: '', is_running: false, run_seq: 1 }],
    })
    const orig = api.get
    api.get = (async () => ({ permissions: [], questions: [], running: false, run_seq: 2 })) as typeof api.get
    try {
      await useSessionStore.getState().fetchPendingRequests('s1')
    } finally {
      api.get = orig
    }
    expect(useSessionStore.getState().sessions[0].run_seq).toBe(2)
  })
})
