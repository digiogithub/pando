import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'

const get = vi.fn()
const put = vi.fn()
vi.mock('@pando/client/services/api', () => ({
  default: { get: (...a: unknown[]) => get(...a), put: (...a: unknown[]) => put(...a) },
}))

import ModelSwitcher from './ModelSwitcher'
import { useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'
import { useSessionModelStore } from '@pando/client/stores/sessionModelStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'

const base = { description: '', badges: [], canReason: false, supportsReasoningEffort: false }

beforeEach(() => {
  get.mockReset()
  put.mockReset()
  useModelAutoModeStore.setState({ autoSelected: false, lastRoutedModel: null })
  useSessionStore.setState({ activeSessionId: null })
  useSessionModelStore.setState({ sessionId: null, selection: null })
  get.mockResolvedValue({
    autoSelected: true,
    models: [
      { ...base, id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
      { ...base, id: 'auto', name: 'Auto', provider: 'auto', routerProvider: 'ollama', routerHealthy: false, routerProblems: ['not reachable'] },
    ],
  })
})

describe('ModelSwitcher Auto entry', () => {
  it('lists Auto first with a health dot and selects it', async () => {
    useModelAutoModeStore.setState({ lastRoutedModel: 'gpt-4o' })
    const { container } = render(<ModelSwitcher />)
    await screen.findByText(/Auto · gpt-4o/)
    const names = [...container.querySelectorAll('.ovl-model-name')].map((n) => n.textContent)
    expect(names[0]).toMatch(/^Auto/)
    const dot = screen.getByTestId('auto-health-dot')
    expect(dot.getAttribute('title')).toMatch(/not reachable/)
    expect(dot.getAttribute('title')).toMatch(/prompts use the coder model\.$/)
    expect(container.querySelector('.ovl-model-name--active')?.textContent).toMatch(/^Auto/)

    fireEvent.click(screen.getByText(/Auto · gpt-4o/))
    // No session yet: the choice is kept for the first prompt, nothing is persisted.
    await waitFor(() => expect(useSessionModelStore.getState().selection).toEqual({ model: null, auto: true }))
    expect(put).not.toHaveBeenCalled()
  })
})

describe('ModelSwitcher session scope', () => {
  it('applies the selection to the active session without touching the default model', async () => {
    get.mockImplementation((url: string) =>
      url.endsWith('/model')
        ? Promise.resolve({ model: 'claude-sonnet-4-6', override: false, autoSelected: false })
        : Promise.resolve({ autoSelected: false, models: [{ ...base, id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' }] }),
    )
    const defaultModel = useSettingsStore.getState().config.default_model
    useSessionStore.setState({ activeSessionId: 's1' })
    render(<ModelSwitcher />)

    put.mockResolvedValue({ model: 'gpt-4o', scope: 'session' })
    fireEvent.click(await screen.findByText('GPT-4o'))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/v1/models/active', { model: 'gpt-4o', sessionId: 's1' }),
    )
    await waitFor(() => expect(useSessionModelStore.getState().selection).toEqual({ model: 'gpt-4o', auto: false }))
    expect(useSettingsStore.getState().config.default_model).toBe(defaultModel)
  })
})
