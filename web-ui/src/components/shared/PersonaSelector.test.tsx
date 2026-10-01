import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'

const get = vi.fn()
const put = vi.fn()

vi.mock('@pando/client/services/api', () => ({
  default: { get: (...a: unknown[]) => get(...a), put: (...a: unknown[]) => put(...a) },
}))

import PersonaSelector, { autoLabel } from './PersonaSelector'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { usePersonaRoutingStore, parsePersonaNotice } from '@pando/client/stores/personaRoutingStore'

function setup(active: Record<string, unknown>, health: unknown = { ok: true, report: { ok: true } }) {
  get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/personas') return { personas: ['assistant', 'software-engineer'] }
    if (path.startsWith('/api/v1/personas/active')) return active
    if (path === '/api/v1/model-auto-mode/router/health') return health
    return {}
  })
}

beforeEach(() => {
  get.mockReset()
  put.mockReset()
  useSessionStore.setState({ activeSessionId: 's1', isStreaming: false })
  usePersonaRoutingStore.setState({ applied: null, noticeCount: 0 })
})

describe('autoLabel / parsePersonaNotice', () => {
  it('formats the Auto entry', () => {
    expect(autoLabel('')).toBe('Auto')
    expect(autoLabel('software-engineer')).toBe('Auto (software-engineer)')
  })
  it('parses the Persona notice', () => {
    expect(parsePersonaNotice('Persona: qa (p=0.90, 12 ms)')).toBe('qa')
    expect(parsePersonaNotice('Auto: x -> y')).toBeNull()
  })
})

describe('PersonaSelector', () => {
  it('shows the applied persona in the Auto label and asks for the session', async () => {
    setup({ active: '', auto: true, applied: 'software-engineer', source: 'decision' })
    render(<PersonaSelector />)
    expect(await screen.findByText('Auto (software-engineer)')).toBeInTheDocument()
    expect(get).toHaveBeenCalledWith('/api/v1/personas/active?sessionId=s1')
  })

  it('follows the Persona notice without waiting for the refetch', async () => {
    setup({ active: '', auto: true, applied: 'assistant' })
    render(<PersonaSelector />)
    expect(await screen.findByText('Auto (assistant)')).toBeInTheDocument()
    act(() => usePersonaRoutingStore.getState().setApplied('qa'))
    expect(await screen.findByText('Auto (qa)')).toBeInTheDocument()
  })

  it('keeps plain Auto for an old server and a manual persona label otherwise', async () => {
    setup({ active: '' })
    const { unmount } = render(<PersonaSelector />)
    await waitFor(() => expect(get).toHaveBeenCalled())
    expect(await screen.findByText('Auto')).toBeInTheDocument()
    unmount()
    setup({ active: 'qa', auto: false })
    render(<PersonaSelector />)
    expect(await screen.findByText('Qa')).toBeInTheDocument()
  })

  it('warns when the decision router is unhealthy', async () => {
    setup(
      { active: '', auto: true, decisionModel: true, applied: '' },
      { ok: false, report: { ok: false, problems: ['not reachable'] } },
    )
    render(<PersonaSelector />)
    expect(await screen.findByTestId('persona-router-warning')).toBeInTheDocument()
  })

  it('shows no warning when the router is healthy', async () => {
    setup({ active: '', auto: true, decisionModel: true })
    render(<PersonaSelector />)
    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/model-auto-mode/router/health'))
    expect(screen.queryByTestId('persona-router-warning')).not.toBeInTheDocument()
  })
})
