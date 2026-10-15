import { beforeEach, describe, expect, it, vi } from 'vitest'
import api from '../services/api'
import type { PermissionRequest, QuestionRequest } from '../types'
import { useSessionStore } from './sessionStore'

const question = { id: 'q1', session_id: 's1', questions: [] } as unknown as QuestionRequest
const permission = { id: 'p1', session_id: 's1' } as unknown as PermissionRequest

describe('sessionStore answered prompts', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useSessionStore.setState({
      activeSessionId: 's1',
      pendingQuestions: [],
      pendingPermissions: [],
      resolvedRequestIds: [],
    })
    vi.spyOn(api, 'post').mockResolvedValue({})
  })

  it('does not re-add an answered question from a later poll or SSE event', async () => {
    const store = useSessionStore.getState()
    store.addQuestionRequest(question)
    await store.respondQuestion('q1', 's1', [])
    expect(useSessionStore.getState().pendingQuestions).toHaveLength(0)

    vi.spyOn(api, 'get').mockResolvedValue({ questions: [question], permissions: [permission] })
    await useSessionStore.getState().fetchPendingRequests('s1')
    useSessionStore.getState().addQuestionRequest(question)

    const after = useSessionStore.getState()
    expect(after.pendingQuestions).toHaveLength(0)
    // the unanswered permission still surfaces
    expect(after.pendingPermissions).toHaveLength(1)
  })

  it('does not re-add a cancelled question or an answered permission', async () => {
    const store = useSessionStore.getState()
    await store.cancelQuestion('q1', 's1')
    await store.respondPermission('p1', 's1', 'allow' as never)

    vi.spyOn(api, 'get').mockResolvedValue({ questions: [question], permissions: [permission] })
    await useSessionStore.getState().fetchPendingRequests('s1')
    useSessionStore.getState().addPermissionRequest(permission)

    const after = useSessionStore.getState()
    expect(after.pendingQuestions).toHaveLength(0)
    expect(after.pendingPermissions).toHaveLength(0)
  })

  it('keeps the resolved list bounded', async () => {
    for (let i = 0; i < 250; i++) await useSessionStore.getState().respondQuestion(`q${i}`, 's1', [])
    expect(useSessionStore.getState().resolvedRequestIds).toHaveLength(200)
  })
})
