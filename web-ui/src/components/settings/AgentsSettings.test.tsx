import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()

vi.mock('@pando/client/services/api', () => ({
  default: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a), put: (...a: unknown[]) => put(...a) },
}))

import AgentsSettings from './AgentsSettings'
import { SETTINGS_CATEGORY_EVENT } from './settingsEvents'
import { useAgentsStore } from '@pando/client/stores/settingsStore'
import { EMPTY_DECISION_DRAFT, useDecisionModelStore } from '@pando/client/stores/decisionModelStore'

function agent(name: string, over: Record<string, unknown> = {}) {
  return { name, model: '', maxTokens: 0, reasoningEffort: '', thinkingMode: '', autoCompact: false, autoCompactThreshold: 0, ...over }
}

function decisionConfig(provider: string, model: string) {
  return {
    router: { provider, baseURL: '', effectiveBaseURL: '', model, keepAlive: '', headers: {}, apiKeySet: false, apiKeyMasked: '' },
    timeoutMs: 0, warnings: [],
  }
}

function setup(opts: { useDecisionModel?: boolean; provider?: string; model?: string }) {
  get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/config/agents') {
      return { agents: [agent('coder'), agent('persona-selector', { useDecisionModel: !!opts.useDecisionModel })] }
    }
    if (path === '/api/v1/config/decision-model') return decisionConfig(opts.provider ?? 'ollama', opts.model ?? '')
    if (path === '/api/v1/models') return { models: [] }
    if (path === '/api/v1/decision-model/router/health') return { ok: true, report: { ok: true, reachable: true } }
    return {}
  })
}

async function openAgent(label: string) {
  fireEvent.click(await screen.findByText(label))
}

beforeEach(() => {
  get.mockReset(); post.mockReset(); put.mockReset()
  useAgentsStore.setState({ agents: [], original: [], dirty: false, loading: false, saving: false, error: null })
  useDecisionModelStore.setState({ draft: EMPTY_DECISION_DRAFT, original: EMPTY_DECISION_DRAFT, dirty: false, loaded: false, health: null, healthError: '' })
})

describe('AgentsSettings persona-selector decision model', () => {
  it('shows the toggle only for persona-selector', async () => {
    setup({})
    render(<AgentsSettings />)
    await openAgent('Coder')
    expect(screen.queryByText(/Use decision model/)).not.toBeInTheDocument()
    await openAgent('Persona Selector')
    expect(screen.getByText(/Use decision model/)).toBeInTheDocument()
    expect(screen.queryByText('Fallback model')).not.toBeInTheDocument()
  })

  it('sends the flag on save and relabels the model as fallback', async () => {
    setup({ model: 'qwen3:1.7b' })
    put.mockResolvedValue({ agents: [] })
    render(<AgentsSettings />)
    await openAgent('Persona Selector')
    fireEvent.click(screen.getByRole('switch', { hidden: true }))
    expect(await screen.findByText('Fallback model')).toBeInTheDocument()
    expect(await screen.findByText('ollama/qwen3:1.7b')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    const body = put.mock.calls[0][1] as { agents: Record<string, unknown>[] }
    expect(body.agents.find((a) => a.name === 'persona-selector')?.useDecisionModel).toBe(true)
    expect(body.agents.find((a) => a.name === 'coder')).not.toHaveProperty('useDecisionModel')
  })

  it('warns with a link when no decision model is configured', async () => {
    setup({ useDecisionModel: true, model: '' })
    render(<AgentsSettings />)
    await openAgent('Persona Selector')
    expect(await screen.findByText(/No decision model is configured/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Configure/ })).toBeInTheDocument()
  })

  it('shows the privacy note only for a hosted router and links to the Decision model page', async () => {
    setup({ useDecisionModel: true, provider: 'typesafe', model: 'jev-latest' })
    const jump = vi.fn()
    window.addEventListener(SETTINGS_CATEGORY_EVENT, jump)
    render(<AgentsSettings />)
    await openAgent('Persona Selector')
    expect(await screen.findByText(/prompts leave your machine/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Configure/ }))
    expect(jump).toHaveBeenCalled()
    expect((jump.mock.calls[0][0] as CustomEvent).detail).toBe('decision-model')
    window.removeEventListener(SETTINGS_CATEGORY_EVENT, jump)
  })

  it('has no privacy note for local ollama', async () => {
    setup({ useDecisionModel: true, provider: 'ollama', model: 'qwen3:1.7b' })
    render(<AgentsSettings />)
    await openAgent('Persona Selector')
    await screen.findByTestId('persona-decision-info')
    await screen.findByText('Healthy')
    expect(screen.queryByText(/prompts leave your machine/i)).not.toBeInTheDocument()
  })
})
