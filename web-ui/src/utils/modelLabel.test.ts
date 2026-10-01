import { describe, expect, it } from 'vitest'
import { activeModelLabel, formatModel } from './modelLabel'

describe('formatModel', () => {
  it('formats known model id shapes', () => {
    expect(formatModel('claude-sonnet-4-6')).toBe('Claude Sonnet 4.6')
    expect(formatModel('copilot.gpt-4o')).toBe('Copilot GPT-4o')
    expect(formatModel('openrouter/qwen3')).toBe('qwen3')
    expect(formatModel('')).toBe('')
  })
})

describe('activeModelLabel', () => {
  it('shows the selected model outside auto mode', () => {
    expect(activeModelLabel('gpt-4o', false, 'claude-sonnet-4-6')).toBe('GPT-4o')
  })

  it('shows Auto until a prompt has been routed', () => {
    expect(activeModelLabel('gpt-4o', true, null)).toBe('Auto')
  })

  it('shows the routed model without route id or probability', () => {
    expect(activeModelLabel('gpt-4o', true, 'copilot.gpt-4o')).toBe('Auto → Copilot GPT-4o')
  })
})
