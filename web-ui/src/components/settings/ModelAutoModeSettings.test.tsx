import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()

vi.mock('@pando/client/services/api', () => ({
  default: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a), put: (...a: unknown[]) => put(...a) },
}))

import ModelAutoModeSettings from './ModelAutoModeSettings'
import { EMPTY_DRAFT, useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'

const SECRET = 'sk-super-secret-1234567890'

function serverConfig(over: Record<string, unknown> = {}) {
  return {
    enabled: true,
    defaultAuto: false,
    autoSelected: false,
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
    threshold: 0.6,
    minConfidence: 0,
    timeoutMs: 0,
    historyPrompts: 0,
    routes: [{ id: 'code', description: 'write code', model: 'gpt-4o', fallbacks: [], disabled: false }],
    warnings: [],
    ...over,
  }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  useModelAutoModeStore.setState({
    draft: EMPTY_DRAFT, original: EMPTY_DRAFT, dirty: false, fieldErrors: [], error: null, warnings: [],
    testResult: null, playground: null, routerModels: null, clearApiKey: false,
  })
  get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/models') {
      return { models: [{ id: 'auto', name: 'Auto', provider: 'auto' }, { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' }] }
    }
    return serverConfig()
  })
})

describe('ModelAutoModeSettings', () => {
  it('renders provider-specific fields', async () => {
    const { container } = render(<ModelAutoModeSettings />)
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
    const { container } = render(<ModelAutoModeSettings />)
    const input = (await screen.findByLabelText('API key')) as HTMLInputElement
    expect(input.type).toBe('password')
    expect(input.value).toBe('')
    expect(screen.getByTestId('ama-key-masked')).toHaveTextContent('••••7890')
    expect(container.innerHTML).not.toContain(SECRET)

    fireEvent.click(screen.getByRole('button', { name: 'Clear key' }))
    put.mockResolvedValue(serverConfig({ router: { ...serverConfig().router, apiKeySet: false, apiKeyMasked: '' } }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({ clearApiKey: true })
    expect(put.mock.calls[0][1].router.apiKey).toBe('')
  })

  it('maps PUT 400 field errors next to fields', async () => {
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Route 1 description')
    fireEvent.change(screen.getByLabelText('Route 1 description'), { target: { value: 'changed' } })
    put.mockRejectedValue(
      new Error(JSON.stringify({
        error: 'invalid modelAutoMode configuration',
        errors: [
          { field: 'modelAutoMode.routes[0].description', message: 'description is too long' },
          { field: 'modelAutoMode.router.model', message: 'model is required' },
        ],
      })),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    const alerts = await screen.findAllByText('description is too long')
    expect(alerts).toHaveLength(1)
    expect(screen.getByText('model is required')).toBeInTheDocument()
  })

  it('shows the Test connection report', async () => {
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Base URL')
    post.mockResolvedValue({
      ok: false,
      problems: ['Model jev-latest not found'],
      report: { ok: false, kind: 'typesafe', reachable: true, authorized: true, versionOK: true, modelPresent: false, isDecisionModel: false, remote: true, latencyMs: 42 },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Test connection' }))
    const report = await screen.findByTestId('ama-test-report')
    expect(report).toHaveTextContent('Reachable')
    expect(report).toHaveTextContent('Model present')
    expect(report).toHaveTextContent('42 ms')
    expect(report).toHaveTextContent('Model jev-latest not found')
    expect(post.mock.calls[0][0]).toBe('/api/v1/model-auto-mode/router/test')
  })

  it('offers Pull for suggested Ollama models and reloads after', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/models') return { models: [] }
      return serverConfig({ router: { ...serverConfig().router, provider: 'ollama', model: 'tev1:0.8b' } })
    })
    const listing = { models: [], status: 'filtered', suggestions: ['nimble'] }
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/router/pull')) return { id: 'j1', target: 'nimble', state: 'done', completed: 0, total: 0 }
      return listing
    })
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Keep alive')
    fireEvent.click(screen.getByRole('button', { name: 'Load models' }))
    const btn = await screen.findByRole('button', { name: 'Pull nimble' })
    listing.suggestions = []
    fireEvent.click(btn)
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Pull nimble' })).not.toBeInTheDocument())
    expect(post.mock.calls.some((c) => c[0] === '/api/v1/model-auto-mode/router/pull')).toBe(true)
  })

  it('renders the playground decision', async () => {
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Playground prompt')
    post.mockResolvedValue({
      decision: {
        routeId: 'code', matched: true, probability: 0.9, confidence: 0.8, reason: 'matched',
        probabilities: { code: 0.9, none: 0.1 }, candidates: ['gpt-4o'], latencyMs: 12, costUsd: 0.000123,
        routerProvider: 'typesafe', routerModel: 'jev-latest',
      },
      usableCandidates: ['gpt-4o'],
      skipped: { 'old-model': 'unknown' },
    })
    fireEvent.change(screen.getByLabelText('Playground prompt'), { target: { value: 'implement a parser' } })
    fireEvent.click(screen.getByRole('button', { name: /Route$/ }))
    const result = await screen.findByTestId('ama-playground-result')
    expect(result).toHaveTextContent('code')
    expect(result).toHaveTextContent('matched')
    expect(result).toHaveTextContent('old-model (unknown)')
    expect(screen.getByRole('progressbar', { name: 'code probability' })).toHaveAttribute('aria-valuenow', '90')
    expect(post.mock.calls[0][1].config.routes[0].id).toBe('code')
  })

  it('adds starter routes without models', async () => {
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Route 1 id')
    fireEvent.click(screen.getByRole('button', { name: 'Add starter routes' }))
    expect(useModelAutoModeStore.getState().draft.routes.map((r) => r.id)).toEqual([
      'code', 'quick_question', 'implementation', 'planning', 'review',
    ])
  })

  it('adds and removes fallbacks with the + button, stacked below the primary model', async () => {
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Route 1 id')
    const routeModels = () => useModelAutoModeStore.getState().draft.routes[0].fallbacks
    expect(screen.queryByLabelText('Route 1 fallback 1')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Add route 1 fallback' }))
    expect(screen.getByLabelText('Route 1 fallback 1')).toBeInTheDocument()
    expect(screen.queryByLabelText('Route 1 fallback 2')).not.toBeInTheDocument()
    // Empty entries are never saved.
    expect(routeModels()).toEqual([])

    fireEvent.click(screen.getByRole('button', { name: 'Add route 1 fallback' }))
    expect(screen.getByLabelText('Route 1 fallback 2')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add route 1 fallback' })).not.toBeInTheDocument()

    // Stacked vertically: primary, fallback 1, fallback 2 in document order inside one column.
    const col = screen.getByTestId('ama-route-0-models')
    expect(col.className).toContain('flex-col')
    const labels = ['Route 1 primary model', 'Route 1 fallback 1', 'Route 1 fallback 2'].map((l) => screen.getByLabelText(l))
    expect(labels[0].compareDocumentPosition(labels[1]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(labels[1].compareDocumentPosition(labels[2]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Remove route 1 fallback 2' }))
    expect(screen.queryByLabelText('Route 1 fallback 2')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add route 1 fallback' })).toBeInTheDocument()
  })

  it('shows existing fallbacks and shifts fallback 2 up when fallback 1 is removed', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/models') return { models: [] }
      return serverConfig({
        routes: [{ id: 'code', description: 'd', model: 'gpt-4o', fallbacks: ['m-a', 'm-b'], disabled: false }],
      })
    })
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Route 1 fallback 2')
    expect(screen.getByLabelText('Route 1 fallback 1')).toHaveTextContent('m-a')
    expect(screen.getByLabelText('Route 1 fallback 2')).toHaveTextContent('m-b')
    expect(screen.queryByRole('button', { name: 'Add route 1 fallback' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Remove route 1 fallback 1' }))
    expect(useModelAutoModeStore.getState().draft.routes[0].fallbacks).toEqual(['m-b'])
    expect(screen.getByLabelText('Route 1 fallback 1')).toHaveTextContent('m-b')
    expect(screen.queryByLabelText('Route 1 fallback 2')).not.toBeInTheDocument()
  })

  it('renders pull suggestions as separate rows', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/models') return { models: [] }
      return serverConfig({ router: { ...serverConfig().router, provider: 'ollama', model: '' } })
    })
    post.mockResolvedValue({ models: [], status: 'filtered', suggestions: ['tev1:0.8b', 'tev1', 'nimble'] })
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Keep alive')
    fireEvent.click(screen.getByRole('button', { name: 'Load models' }))
    const list = await screen.findByTestId('pull-suggestions')
    const rows = list.querySelectorAll('li')
    expect(rows).toHaveLength(3)
    rows.forEach((row) => expect(row.querySelectorAll('button')).toHaveLength(1))
    expect(rows[0]).toHaveTextContent('tev1:0.8b')
    expect(rows[2]).toHaveTextContent('nimble')
  })
})
