import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()

vi.mock('@pando/client/services/api', () => ({
  default: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a), put: (...a: unknown[]) => put(...a) },
}))

import DecisionModelSettings from './DecisionModelSettings'
import { EMPTY_DECISION_DRAFT, useDecisionModelStore } from '@pando/client/stores/decisionModelStore'

const SECRET = 'sk-super-secret-1234567890'

function serverConfig(over: Record<string, unknown> = {}) {
  return {
    router: {
      provider: 'typesafe',
      baseURL: '',
      effectiveBaseURL: 'https://api.typesafe.ai',
      model: 'jev-latest',
      keepAlive: '',
      headers: {},
      apiKeySet: true,
      apiKeyMasked: '••••7890',
    },
    timeoutMs: 0,
    warnings: [],
    ...over,
  }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  useDecisionModelStore.setState({
    draft: EMPTY_DECISION_DRAFT, original: EMPTY_DECISION_DRAFT, dirty: false, fieldErrors: [], error: null,
    warnings: [], testResult: null, routerModels: null, clearApiKey: false, pulls: {}, loaded: false,
  })
  get.mockImplementation(async () => serverConfig())
})

describe('DecisionModelSettings', () => {
  it('loads from the decision-model endpoint', async () => {
    render(<DecisionModelSettings />)
    await screen.findByLabelText('Base URL')
    expect(get).toHaveBeenCalledWith('/api/v1/config/decision-model')
  })

  it('renders provider-specific fields', async () => {
    const { container } = render(<DecisionModelSettings />)
    await screen.findByLabelText('Base URL')
    // TypeSafe: API key is shown, keep-alive (Ollama only) is not; remote privacy notice is shown.
    expect(screen.getByLabelText('API key')).toBeInTheDocument()
    expect(screen.queryByLabelText('Keep alive')).not.toBeInTheDocument()
    expect(screen.getByText(/prompts leave your machine/i)).toBeInTheDocument()
    expect(screen.getByLabelText('Base URL')).toHaveAttribute('placeholder', 'https://api.typesafe.ai')

    fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'ollama' } })
    expect(screen.getByLabelText('Keep alive')).toBeInTheDocument()
    expect(screen.queryByLabelText('API key')).not.toBeInTheDocument()
    expect(container.textContent).not.toMatch(/prompts leave your machine/i)

    fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'custom' } })
    expect(screen.getByLabelText('Preset')).toBeInTheDocument()
    expect(screen.getByText('Extra headers')).toBeInTheDocument()
  })

  it('never shows the stored key in plain text and offers clear', async () => {
    const { container } = render(<DecisionModelSettings />)
    const input = (await screen.findByLabelText('API key')) as HTMLInputElement
    expect(input.type).toBe('password')
    expect(input.value).toBe('')
    expect(screen.getByTestId('dm-key-masked')).toHaveTextContent('••••7890')
    expect(container.innerHTML).not.toContain(SECRET)

    fireEvent.click(screen.getByRole('button', { name: 'Clear key' }))
    put.mockResolvedValue(serverConfig({ router: { ...serverConfig().router, apiKeySet: false, apiKeyMasked: '' } }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][0]).toBe('/api/v1/config/decision-model')
    expect(put.mock.calls[0][1]).toMatchObject({ clearApiKey: true })
    expect(put.mock.calls[0][1].router.apiKey).toBe('')
  })

  it('saves the timeout and the router, without model-auto-mode fields', async () => {
    render(<DecisionModelSettings />)
    await screen.findByLabelText('Timeout (ms)')
    fireEvent.change(screen.getByLabelText('Timeout (ms)'), { target: { value: '2500' } })
    put.mockResolvedValue(serverConfig({ timeoutMs: 2500 }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(body.timeoutMs).toBe(2500)
    expect(body).toHaveProperty('router')
    expect(body).not.toHaveProperty('routes')
    expect(body).not.toHaveProperty('enabled')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled())
  })

  it('maps PUT 400 field errors next to fields', async () => {
    render(<DecisionModelSettings />)
    await screen.findByLabelText('Timeout (ms)')
    fireEvent.change(screen.getByLabelText('Timeout (ms)'), { target: { value: '-5' } })
    put.mockRejectedValue(
      new Error(JSON.stringify({
        error: 'invalid decisionModel configuration',
        errors: [
          { field: 'decisionModel.timeoutMs', message: 'timeout must not be negative' },
          { field: 'decisionModel.router.model', message: 'model is required' },
        ],
      })),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('timeout must not be negative')).toBeInTheDocument()
    expect(screen.getByText('model is required')).toBeInTheDocument()
  })

  it('shows the Test connection report', async () => {
    render(<DecisionModelSettings />)
    await screen.findByLabelText('Base URL')
    post.mockResolvedValue({
      ok: false,
      problems: ['Model jev-latest not found'],
      report: { ok: false, kind: 'typesafe', reachable: true, authorized: true, versionOK: true, modelPresent: false, isDecisionModel: false, remote: true, latencyMs: 42 },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    const report = await screen.findByTestId('dm-test-report')
    expect(report).toHaveTextContent('Reachable')
    expect(report).toHaveTextContent('Model present')
    expect(report).toHaveTextContent('42 ms')
    expect(report).toHaveTextContent('Model jev-latest not found')
    expect(post.mock.calls[0][0]).toBe('/api/v1/decision-model/router/test')
  })

  it('offers Pull for suggested Ollama models and reloads after', async () => {
    get.mockImplementation(async () => serverConfig({ router: { ...serverConfig().router, provider: 'ollama', model: 'tev1:0.8b' } }))
    const listing = { models: [], status: 'filtered', suggestions: ['nimble'] }
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/router/pull')) return { id: 'j1', target: 'nimble', state: 'done', completed: 0, total: 0 }
      return listing
    })
    render(<DecisionModelSettings />)
    await screen.findByLabelText('Keep alive')
    fireEvent.click(screen.getByRole('button', { name: 'Load models' }))
    const btn = await screen.findByRole('button', { name: 'Pull nimble' })
    expect(post.mock.calls[0][0]).toBe('/api/v1/decision-model/router/models')
    listing.suggestions = []
    fireEvent.click(btn)
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Pull nimble' })).not.toBeInTheDocument())
    expect(post.mock.calls.some((c) => c[0] === '/api/v1/decision-model/router/pull')).toBe(true)
  })

  it('renders pull suggestions as separate rows', async () => {
    get.mockImplementation(async () => serverConfig({ router: { ...serverConfig().router, provider: 'ollama', model: '' } }))
    post.mockResolvedValue({ models: [], status: 'filtered', suggestions: ['tev1:0.8b', 'tev1', 'nimble'] })
    render(<DecisionModelSettings />)
    await screen.findByLabelText('Keep alive')
    fireEvent.click(screen.getByRole('button', { name: 'Load models' }))
    const list = await screen.findByTestId('pull-suggestions')
    const rows = list.querySelectorAll('li')
    expect(rows).toHaveLength(3)
    rows.forEach((row) => expect(row.querySelectorAll('button')).toHaveLength(1))
    expect(rows[0]).toHaveTextContent('tev1:0.8b')
    expect(rows[2]).toHaveTextContent('nimble')
  })

  it('adds and removes custom headers', async () => {
    get.mockImplementation(async () => serverConfig({ router: { ...serverConfig().router, provider: 'custom' } }))
    render(<DecisionModelSettings />)
    await screen.findByLabelText('New header name')
    fireEvent.change(screen.getByLabelText('New header name'), { target: { value: 'X-Team' } })
    fireEvent.change(screen.getByLabelText('New header value'), { target: { value: 'core' } })
    fireEvent.click(screen.getByRole('button', { name: /Add/ }))
    expect(useDecisionModelStore.getState().draft.router.headers).toEqual({ 'X-Team': 'core' })
    fireEvent.click(screen.getByRole('button', { name: 'Remove header X-Team' }))
    expect(useDecisionModelStore.getState().draft.router.headers).toEqual({})
  })
})
