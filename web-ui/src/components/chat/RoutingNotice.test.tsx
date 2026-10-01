import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import MessageBubble from './MessageBubble'
import { handleSystemMessageEvent, handleDelegationEvent } from '@pando/client/hooks/routingNotice'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useModelAutoModeStore } from '@pando/client/stores/modelAutoModeStore'
import type { Message } from '@pando/client/types'

const asst: Message = {
  id: 'a1', session_id: 's', role: 'assistant', content: [{ type: 'text', text: '' }], created_at: new Date().toISOString(),
}

beforeEach(() => {
  useSessionStore.setState({ messages: [asst] })
  useModelAutoModeStore.setState({ lastRoutedModel: null })
})

describe('routing notice', () => {
  it('stores a routing row before the streaming bubble and updates lastRoutedModel', () => {
    handleSystemMessageEvent({
      type: 'system_message', session_id: 's', message: 'Auto: code → m (p=0.93)\n',
      routing: { routeId: 'code', model: 'gpt-4o', probability: 0.93, kind: 'routed' },
    }, 's')
    const msgs = useSessionStore.getState().messages
    expect(msgs.map((m) => m.role)).toEqual(['system', 'assistant'])
    expect(msgs[0].routing?.model).toBe('gpt-4o')
    expect(useModelAutoModeStore.getState().lastRoutedModel).toBe('gpt-4o')
  })

  it('renders a compact chip and highlights failover', () => {
    const base = { session_id: 's', role: 'system' as const, created_at: new Date().toISOString() }
    const { rerender } = render(
      <MessageBubble message={{ ...base, id: '1', content: [{ type: 'text', text: 'x' }],
        routing: { kind: 'routed', model: 'gpt-4o', routeId: 'code', probability: 0.93 } }} />,
    )
    const row = screen.getByTestId('routing-row')
    expect(row).toHaveTextContent('Auto → gpt-4o · code · p=0.93')
    expect(row.className).not.toMatch(/--warn/)

    rerender(
      <MessageBubble message={{ ...base, id: '2', content: [{ type: 'text', text: 'Auto: A failed (rate_limit), retrying on B\n' }],
        routing: { kind: 'failover', model: 'B' } }} />,
    )
    expect(screen.getByTestId('routing-row').className).toMatch(/--warn/)
    expect(screen.getByTestId('routing-row')).toHaveTextContent('retrying on B')
  })

  it('shows generic system messages as a muted row without touching lastRoutedModel', () => {
    handleSystemMessageEvent({ type: 'system_message', message: 'hello' }, 's')
    const m = useSessionStore.getState().messages[0]
    expect(m.notice).toBe(true)
    expect(useModelAutoModeStore.getState().lastRoutedModel).toBeNull()
    render(<MessageBubble message={m} />)
    expect(screen.getByTestId('notice-row')).toHaveTextContent('hello')
  })
})

describe('delegation notices', () => {
  it('inserts a translatable resuming marker before the streaming bubble', () => {
    expect(handleDelegationEvent({ type: 'resurrected', session_id: 's' }, 's')).toBe(true)
    const msgs = useSessionStore.getState().messages
    expect(msgs.map((m) => m.role)).toEqual(['system', 'assistant'])
    expect(msgs[0].notice).toBe(true)
    expect(msgs[0].noticeKey).toBe('chat.notice.resurrected')
    render(<MessageBubble message={msgs[0]} />)
    expect(screen.getByTestId('notice-row')).toBeInTheDocument()
  })

  it('does not stack the resuming marker when the same run is replayed twice', () => {
    handleDelegationEvent({ type: 'resurrected', session_id: 's' }, 's')
    handleDelegationEvent({ type: 'resurrected', session_id: 's' }, 's')
    expect(useSessionStore.getState().messages.filter((m) => m.noticeKey === 'chat.notice.resurrected')).toHaveLength(1)
  })

  it('maps conclusion events and ignores unrelated ones', () => {
    expect(handleDelegationEvent({ type: 'conclusion_queued' }, 's')).toBe(true)
    expect(handleDelegationEvent({ type: 'conclusion_injected' }, 's')).toBe(true)
    expect(handleDelegationEvent({ type: 'content_delta' }, 's')).toBe(false)
    expect(useSessionStore.getState().messages.map((m) => m.noticeKey).filter(Boolean)).toEqual([
      'chat.notice.conclusionQueued', 'chat.notice.conclusionInjected',
    ])
  })
})
