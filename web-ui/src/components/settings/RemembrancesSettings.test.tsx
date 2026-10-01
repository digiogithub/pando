import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()

vi.mock('@pando/client/services/api', () => ({
  default: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a), put: (...a: unknown[]) => put(...a) },
}))

import RemembrancesSettings from './RemembrancesSettings'
import { SETTINGS_CATEGORY_EVENT } from './settingsEvents'
import { useServicesSettingsStore } from '@pando/client/stores/servicesSettingsStore'
import { EMPTY_DECISION_DRAFT, useDecisionModelStore } from '@pando/client/stores/decisionModelStore'

function decisionConfig(provider: string, model: string) {
  return {
    router: { provider, baseURL: '', effectiveBaseURL: '', model, keepAlive: '', headers: {}, apiKeySet: false, apiKeyMasked: '' },
    timeoutMs: 0, warnings: [],
  }
}

function setup(opts: { provider?: string; model?: string; remembrances?: Record<string, unknown> } = {}) {
  get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/config/services') {
      return {
        remembrances: {
          context_enrichment_decision_filter_enabled: false,
          memory_context_decision_filter_enabled: false,
          context_enrichment_decision_filter_threshold: 0.6,
          context_enrichment_decision_filter_max_candidates: 32,
          context_enrichment_decision_filter_max_candidate_chars: 400,
          context_enrichment_decision_filter_allow_hosted: false,
          ...opts.remembrances,
        },
      }
    }
    if (path === '/api/v1/config/decision-model') return decisionConfig(opts.provider ?? 'ollama', opts.model ?? 'tev1:0.8b')
    if (path === '/api/v1/decision-model/router/health') return { ok: true, report: { ok: true, reachable: true } }
    return {}
  })
}

beforeEach(() => {
  get.mockReset(); post.mockReset(); put.mockReset()
  useDecisionModelStore.setState({ draft: EMPTY_DECISION_DRAFT, original: EMPTY_DECISION_DRAFT, dirty: false, loaded: false, health: null, healthError: '' })
  useServicesSettingsStore.setState({ dirty: false, error: null })
})

describe('RemembrancesSettings decision model filter', () => {
  it('shows the decision model in use and the new controls with their defaults', async () => {
    setup()
    render(<RemembrancesSettings />)
    const block = await screen.findByTestId('rem-decision-filter')
    expect(await screen.findByTestId('rem-decision-in-use')).toHaveTextContent('ollama/tev1:0.8b')
    expect(await screen.findByText('Healthy')).toBeInTheDocument()
    expect(block).toHaveTextContent('Filter retrieved context with the decision model')
    expect(block).toHaveTextContent('Filter injected memories with the decision model')
    expect(screen.getByLabelText('Relevance threshold')).toHaveValue(0.6)
    expect(screen.getByLabelText('Max candidates')).toHaveValue(32)
    expect(screen.getByLabelText('Max characters per candidate')).toHaveValue(400)
    expect(screen.getByRole('switch', { name: /Allow hosted decision providers \(sends snippets to the provider\)/ })).not.toBeChecked()
  })

  it('warns with a link when a filter is on and no decision model is configured', async () => {
    setup({ model: '', remembrances: { context_enrichment_decision_filter_enabled: true } })
    const jump = vi.fn()
    window.addEventListener(SETTINGS_CATEGORY_EVENT, jump)
    render(<RemembrancesSettings />)
    expect(await screen.findByText(/No decision model is configured/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Configure/ }))
    expect((jump.mock.calls[0][0] as CustomEvent).detail).toBe('decision-model')
    window.removeEventListener(SETTINGS_CATEGORY_EVENT, jump)
  })

  it('shows the snippet privacy hint for a hosted provider', async () => {
    setup({ provider: 'typesafe', model: 'jev-latest' })
    render(<RemembrancesSettings />)
    expect(await screen.findByText(/snippets are sent to it/i)).toBeInTheDocument()
  })

  it('persists the toggles through the services save path', async () => {
    setup()
    put.mockResolvedValue({})
    render(<RemembrancesSettings />)
    await screen.findByTestId('rem-decision-filter')
    fireEvent.click(screen.getByRole('switch', { name: /Filter retrieved context with the decision model/ }))
    fireEvent.click(screen.getByRole('switch', { name: /Filter injected memories with the decision model/ }))
    fireEvent.click(screen.getByRole('switch', { name: /Allow hosted decision providers/ }))
    fireEvent.change(screen.getByLabelText('Relevance threshold'), { target: { value: '0.8' } })
    fireEvent.change(screen.getByLabelText('Max candidates'), { target: { value: '16' } })
    fireEvent.change(screen.getByLabelText('Max characters per candidate'), { target: { value: '300' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][0]).toBe('/api/v1/config/services')
    expect(put.mock.calls[0][1].remembrances).toMatchObject({
      context_enrichment_decision_filter_enabled: true,
      memory_context_decision_filter_enabled: true,
      context_enrichment_decision_filter_allow_hosted: true,
      context_enrichment_decision_filter_threshold: 0.8,
      context_enrichment_decision_filter_max_candidates: 16,
      context_enrichment_decision_filter_max_candidate_chars: 300,
    })
  })

  it('flags an invalid threshold locally and shows the server validation error', async () => {
    setup()
    render(<RemembrancesSettings />)
    await screen.findByTestId('rem-decision-filter')
    fireEvent.change(screen.getByLabelText('Relevance threshold'), { target: { value: '1.5' } })
    expect(screen.getByText(/greater than 0 and at most 1/)).toBeInTheDocument()
    put.mockRejectedValue(
      new Error(JSON.stringify({ error: 'context_enrichment_decision_filter_threshold must be in (0, 1], got 1.5' })),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    const alert = await screen.findByText(/threshold must be in \(0, 1\], got 1\.5/)
    expect(alert).toBeInTheDocument()
    expect(alert.textContent).not.toContain('{"error"')
  })
})
