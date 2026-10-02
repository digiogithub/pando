import '@testing-library/jest-dom/vitest'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import MessageList from './MessageList'
import type { StreamingState } from '@pando/client/hooks/useChat'
import type { Message } from '@pando/client/types'

const idle: StreamingState = { thinking: '', toolCalls: [], plan: [], items: [], goal: null }

const msg = (id: string, role: Message['role'], text: string): Message => ({
  id, session_id: 's', role, content: [{ type: 'text', text }], created_at: new Date().toISOString(),
})

const thread = (answer: string) => [msg('u1', 'user', 'hi'), msg('a1', 'assistant', answer)]

/** jsdom has no layout: give the scroller a geometry and record scrollTo calls. */
function mockGeometry(el: HTMLElement, scrollHeight: number, clientHeight = 400) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: scrollHeight })
  Object.defineProperty(el, 'clientHeight', { configurable: true, value: clientHeight })
}

function userScrollTo(el: HTMLElement, top: number) {
  el.scrollTop = top
  fireEvent.scroll(el)
}

const scrollTo = vi.fn()

beforeEach(() => {
  scrollTo.mockClear()
  Element.prototype.scrollTo = scrollTo as unknown as typeof Element.prototype.scrollTo
})

function setup() {
  const view = render(<MessageList messages={thread('a')} streaming streamingState={idle} />)
  const scroller = view.container.querySelector('.chat-scroll') as HTMLElement
  mockGeometry(scroller, 2000)
  userScrollTo(scroller, 1600) // at the bottom
  scrollTo.mockClear()
  const stream = (answer: string) =>
    view.rerender(<MessageList messages={thread(answer)} streaming streamingState={idle} />)
  return { view, scroller, stream }
}

describe('MessageList auto-scroll', () => {
  it('follows streamed content while the reader is at the bottom', () => {
    const { stream } = setup()
    stream('ab')
    expect(scrollTo).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: /bottom/i })).not.toBeInTheDocument()
  })

  it('stops following as soon as the reader scrolls up, even within the threshold', () => {
    const { scroller, stream } = setup()
    userScrollTo(scroller, 1570) // 30px up: still inside the stick threshold
    stream('ab')
    stream('abc')
    expect(scrollTo).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /bottom/i })).toBeInTheDocument()
  })

  it('resumes following when the reader scrolls back to the bottom', () => {
    const { scroller, stream } = setup()
    userScrollTo(scroller, 800)
    userScrollTo(scroller, 1590)
    stream('ab')
    expect(scrollTo).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: /bottom/i })).not.toBeInTheDocument()
  })

  it('resumes following from the scroll-to-bottom button', () => {
    const { scroller, stream } = setup()
    userScrollTo(scroller, 800)
    fireEvent.click(screen.getByRole('button', { name: /bottom/i }))
    expect(scrollTo).toHaveBeenCalledTimes(1)
    stream('ab')
    expect(scrollTo).toHaveBeenCalledTimes(2)
  })

  it('keeps following through a smooth scroll that is still far from the bottom', () => {
    const { scroller, stream } = setup()
    mockGeometry(scroller, 3000) // content grew; the animation is mid-flight
    userScrollTo(scroller, 1800)
    stream('ab')
    expect(scrollTo).toHaveBeenCalledTimes(1)
  })

  it('does not treat a shrinking thread as the reader scrolling up', () => {
    const { scroller, stream } = setup()
    mockGeometry(scroller, 1500) // content collapsed; the browser clamps scrollTop
    userScrollTo(scroller, 1100)
    stream('ab')
    expect(scrollTo).toHaveBeenCalledTimes(1)
  })

  it('a newly sent user message resumes following', () => {
    const { view, scroller } = setup()
    userScrollTo(scroller, 800)
    view.rerender(
      <MessageList messages={[...thread('a'), msg('u2', 'user', 'more')]} streaming streamingState={idle} />,
    )
    expect(scrollTo).toHaveBeenCalledTimes(1)
  })
})
