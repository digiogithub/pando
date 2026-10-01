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
import { EMPTY_DECISION_DRAFT, useDecisionModelStore } from '@pando/client/stores/decisionModelStore'
import { SETTINGS_CATEGORY_EVENT } from './settingsEvents'

function decisionConfig(over: Record<string, unknown> = {}) {
  return {
    router: {
      provider: 'typesafe', baseURL: '', effectiveBaseURL: 'https://api.typesafe.ai', model: 'jev-latest',
      keepAlive: '', headers: {}, apiKeySet: true, apiKeyMasked: '••••7890',
    },
    timeoutMs: 0,
    warnings: [],
    ...over,
  }
}

function serverConfig(over: Record<string, unknown> = {}) {
  return {
    enabled: true,
    defaultAuto: false,
    autoSelected: false,
    // Deprecated alias fields the server still returns: the page must ignore them.
    router: decisionConfig().router,
    timeoutMs: 1234,
    threshold: 0.6,
    minConfidence: 0,
    historyPrompts: 0,
    routes: [{ id: 'code', description: 'write code', model: 'gpt-4o', fallbacks: [], disabled: false }],
    warnings: [],
    ...over,
  }
}

function mockGets(opts: { decision?: Record<string, unknown>; auto?: Record<string, unknown> } = {}) {
  get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/models') {
      return { models: [{ id: 'auto', name: 'Auto', provider: 'auto' }, { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' }] }
    }
    if (path === '/api/v1/config/decision-model') return decisionConfig(opts.decision)
    if (path === '/api/v1/decision-model/router/health') return { ok: true, report: { ok: true, reachable: true } }
    return serverConfig(opts.auto)
  })
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  useModelAutoModeStore.setState({
    draft: EMPTY_DRAFT, original: EMPTY_DRAFT, dirty: false, fieldErrors: [], error: null, warnings: [],
    playground: null,
  })
  useDecisionModelStore.setState({
    draft: EMPTY_DECISION_DRAFT, original: EMPTY_DECISION_DRAFT, dirty: false, loaded: false, health: null, healthError: '',
  })
  mockGets()
})

describe('ModelAutoModeSettings', () => {
  it('has no provider fields and shows the decision model in use with a Configure link', async () => {
    const jump = vi.fn()
    window.addEventListener(SETTINGS_CATEGORY_EVENT, jump)
    render(<ModelAutoModeSettings />)
    const row = await screen.findByTestId('ama-decision-in-use')
    expect(row).toHaveTextContent('typesafe/jev-latest')
    expect(await screen.findByText('Healthy')).toBeInTheDocument()
    expect(screen.queryByLabelText('Provider')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Base URL')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('API key')).not.toBeInTheDocument()
    expect(screen.queryByText('Timeout (ms)')).not.toBeInTheDocument()
    // Hosted provider: privacy hint is part of the shared row.
    expect(row).toHaveTextContent(/prompts leave your machine/i)
    fireEvent.click(screen.getByRole('button', { name: /Configure/ }))
    expect(jump).toHaveBeenCalled()
    expect((jump.mock.calls[0][0] as CustomEvent).detail).toBe('decision-model')
    window.removeEventListener(SETTINGS_CATEGORY_EVENT, jump)
    expect(get.mock.calls.some((c) => c[0] === '/api/v1/decision-model/router/health')).toBe(true)
  })

  it('warns when Auto mode is enabled and no decision model is configured', async () => {
    mockGets({ decision: { router: { ...decisionConfig().router, model: '', provider: 'ollama' } } })
    render(<ModelAutoModeSettings />)
    expect(await screen.findByText(/No decision model is configured/)).toBeInTheDocument()
    expect(screen.queryByText(/prompts leave your machine/i)).not.toBeInTheDocument()
  })

  it('saves without router or timeoutMs', async () => {
    render(<ModelAutoModeSettings />)
    await screen.findByLabelText('Route 1 description')
    fireEvent.change(screen.getByLabelText('Route 1 description'), { target: { value: 'changed' } })
    put.mockResolvedValue(serverConfig())
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][0]).toBe('/api/v1/config/model-auto-mode')
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(body).not.toHaveProperty('router')
    expect(body).not.toHaveProperty('timeoutMs')
    expect(body).not.toHaveProperty('clearApiKey')
    expect(body).toMatchObject({ enabled: true, threshold: 0.6 })
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
    expect(post.mock.calls[0][0]).toBe('/api/v1/model-auto-mode/playground')
    expect(post.mock.calls[0][1].config.routes[0].id).toBe('code')
    expect(post.mock.calls[0][1].config).not.toHaveProperty('router')
    expect(post.mock.calls[0][1]).not.toHaveProperty('decision')
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
    mockGets({ auto: { routes: [{ id: 'code', description: 'd', model: 'gpt-4o', fallbacks: ['m-a', 'm-b'], disabled: false }] } })
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
})
