import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import MessageBubble from './MessageBubble'
import ContextFilterNotice from './ContextFilterNotice'
import { handleSystemMessageEvent } from '@pando/client/hooks/routingNotice'
import { parseSSEPayload } from '@pando/client/services/sse'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import type { ContextFilterInfo, Message } from '@pando/client/types'

const info: ContextFilterInfo = {
  kept: 4,
  dropped: 5,
  bySource: { code: { kept: 1, dropped: 2 }, kb: { kept: 2, dropped: 3 }, events: { kept: 1, dropped: 0 } },
  threshold: 0.6,
  latencyMs: 38,
  routerProvider: 'ollama',
  routerModel: 'tev1:0.8b',
  notice: 'Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/5, events 1/1',
}

const asst: Message = {
  id: 'a1', session_id: 's', role: 'assistant', content: [{ type: 'text', text: '' }], created_at: new Date().toISOString(),
}

beforeEach(() => {
  useSessionStore.setState({ messages: [asst] })
  useLayoutStore.setState({ chatMode: 'advanced' })
})

describe('ContextFilterNotice', () => {
  it('renders the compact line and expands the per-source counts', () => {
    render(<ContextFilterNotice info={info} />)
    const row = screen.getByTestId('context-filter-row')
    expect(row).toHaveTextContent('Context filter: kept 4/9 · 38 ms')
    expect(screen.queryByTestId('context-filter-details')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Show details' }))
    expect(screen.getByTestId('context-filter-source-code')).toHaveTextContent('Code: kept 1/3')
    expect(screen.getByTestId('context-filter-source-kb')).toHaveTextContent('KB: kept 2/5')
    expect(screen.getByTestId('context-filter-source-events')).toHaveTextContent('Events: kept 1/1')
    expect(screen.getByTestId('context-filter-router')).toHaveTextContent('ollama/tev1:0.8b')
    expect(screen.queryByTestId('context-filter-partial')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Hide details' }))
    expect(screen.queryByTestId('context-filter-details')).not.toBeInTheDocument()
  })

  it('highlights and explains a partial result, with router fields omitted', () => {
    render(<ContextFilterNotice info={{ kept: 3, dropped: 0, reason: 'partial:timeout' }} />)
    const line = screen.getByTestId('context-filter-row').firstElementChild as HTMLElement
    expect(line.className).toMatch(/--warn/)
    expect(screen.getByTestId('context-filter-row')).toHaveTextContent('kept 3/3')
    fireEvent.click(screen.getByRole('button', { name: 'Show details' }))
    expect(screen.getByTestId('context-filter-partial')).toHaveTextContent('timeout')
    expect(screen.queryByTestId('context-filter-router')).not.toBeInTheDocument()
  })

  it('has no toggle when there is nothing more to show', () => {
    render(<ContextFilterNotice info={{ kept: 2, dropped: 0 }} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('in simple chat mode is hidden unless something was dropped', () => {
    useLayoutStore.setState({ chatMode: 'simple' })
    const { rerender } = render(<ContextFilterNotice info={{ ...info, kept: 9, dropped: 0 }} />)
    expect(screen.queryByTestId('context-filter-row')).not.toBeInTheDocument()
    rerender(<ContextFilterNotice info={info} />)
    expect(screen.getByTestId('context-filter-row')).toBeInTheDocument()
  })
})

describe('context filter SSE event', () => {
  it('keeps the context_filter payload on the message and renders it through MessageBubble', () => {
    const event = parseSSEPayload('system_message', {
      session_id: 's',
      text: `${info.notice}\n`,
      context_filter: info,
    })
    expect(event.context_filter?.kept).toBe(4)
    handleSystemMessageEvent(event, 's')
    const msgs = useSessionStore.getState().messages
    expect(msgs.map((m) => m.role)).toEqual(['system', 'assistant'])
    expect(msgs[0].contextFilter?.bySource?.kb).toEqual({ kept: 2, dropped: 3 })
    expect(msgs[0].routing).toBeUndefined()
    expect(msgs[0].notice).toBe(false)
    render(<MessageBubble message={msgs[0]} />)
    expect(screen.getByTestId('context-filter-row')).toHaveTextContent('kept 4/9')
  })

  it('renders a fail-open warning as a plain system notice', () => {
    const event = parseSSEPayload('system_message', {
      session_id: 's',
      text: 'Context filter unavailable (unreachable): context injected unfiltered\n',
    })
    handleSystemMessageEvent(event, 's')
    const m = useSessionStore.getState().messages[0]
    expect(m.contextFilter).toBeUndefined()
    render(<MessageBubble message={m} />)
    expect(screen.getByTestId('notice-row')).toHaveTextContent('Context filter unavailable')
  })
})
