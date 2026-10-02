import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Message } from '@pando/client/types'
import type { StreamingState } from '@pando/client/hooks/useChat'
import { useChatDraftStore } from '@pando/client/stores/chatDraftStore'
import { Button } from '@/components/ui'
import { BrandMark } from '@/components/brand'
import { ArrowDown, FlaskConical, GitCompare, Hourglass, Bug, Search } from '@/components/ui/icons'
import MessageBubble, { AssistantTurn, EventRow, ThinkingShimmer } from './MessageBubble'

/** Distance from the bottom (px) under which the view counts as "at the bottom". */
const STICK_THRESHOLD = 80

// ─── Empty state ──────────────────────────────────────────────────────────────

const SUGGESTIONS = [
  { key: 'explain', icon: Search },
  { key: 'bug', icon: Bug },
  { key: 'tests', icon: FlaskConical },
  { key: 'review', icon: GitCompare },
] as const

/**
 * Greeting shown when the session has no messages yet. The views render it,
 * then the composer, then <ChatSuggestions/>, inside a `.chat-pane--empty`
 * pane that centers the three (Claude-Desktop-like). The composer keeps the
 * same slot in both states so it is never remounted (focus survives sending).
 */
export function ChatEmptyHead() {
  const { t } = useTranslation()
  return (
    <div className="chat-column">
      <div className="chat-empty-head">
        <BrandMark size={40} className="chat-empty-mark" />
        <h1 className="chat-empty-title brand-display">{t('chat.empty.title')}</h1>
      </div>
    </div>
  )
}

/** Quiet starter prompts under the empty-state composer. */
export function ChatSuggestions() {
  const { t } = useTranslation()
  const insertIntoDraft = useChatDraftStore((s) => s.insertIntoDraft)
  return (
    <div className="chat-column">
      <div className="chat-suggestions">
        {SUGGESTIONS.map(({ key, icon: Icon }) => (
          <button
            key={key}
            type="button"
            className="chat-suggestion"
            onClick={() => insertIntoDraft(t(`chat.empty.prompts.${key}`))}
          >
            <Icon size={14} />
            {t(`chat.empty.suggestions.${key}`)}
          </button>
        ))}
      </div>
    </div>
  )
}

// ─── Loading bubble (shown when no assistant message exists yet) ───────────────

function LoadingBubble({ streamingState }: { streamingState: StreamingState }) {
  const hasThinking = streamingState.thinking.length > 0
  const hasTools = streamingState.toolCalls.length > 0

  if (!hasThinking && !hasTools) {
    return <div className="chat-msg chat-msg--assistant"><ThinkingShimmer /></div>
  }

  return (
    <div className="chat-msg chat-msg--assistant">
      <div className="chat-activity">
        {hasThinking && <EventRow kind="thinking" thinking={streamingState.thinking} isLive />}
        {streamingState.toolCalls.map((tc) => (
          <EventRow
            key={tc.id}
            kind="tool"
            toolName={tc.name}
            toolInput={(() => { try { return JSON.parse(tc.input) } catch { return null } })()}
            toolResult={tc.result?.content}
            isError={tc.is_error}
            isLive={tc.status === 'pending' || tc.status === 'in_progress'}
            backendTitle={tc.title}
            backendKind={tc.kind}
            toolStatus={tc.status}
            locations={tc.locations}
            diff={tc.diff}
            terminal={tc.terminal}
            images={tc.result?.images}
          />
        ))}
      </div>
    </div>
  )
}

// ─── Queued feedback chips ────────────────────────────────────────────────────

/**
 * Mid-run feedback the user submitted while the agent was busy. It sits here
 * until the agent loop reaches a safe boundary and injects it, at which point
 * useChat turns it into a real user message in the transcript.
 */
function QueuedFeedback({ items }: { items: string[] }) {
  const { t } = useTranslation()
  return (
    <>
      {items.map((text, i) => (
        <div key={i} className="chat-queued" title={t('chat.queuedHint')}>
          <span className="chat-queued-label"><Hourglass size={12} />{t('chat.queued')}</span>
          <span className="chat-queued-text">{text}</span>
        </div>
      ))}
    </>
  )
}

// ─── Main component ───────────────────────────────────────────────────────────

type ThreadItem =
  | { type: 'single'; message: Message }
  | { type: 'turn'; messages: Message[]; last: boolean }

/** Consecutive assistant messages (one per agent-loop step) form one turn. */
function groupTurns(messages: Message[]): ThreadItem[] {
  const items: ThreadItem[] = []
  messages.forEach((m, i) => {
    const isLast = i === messages.length - 1
    if (m.role !== 'assistant') {
      items.push({ type: 'single', message: m })
      return
    }
    const prev = items[items.length - 1]
    if (prev && prev.type === 'turn') {
      prev.messages.push(m)
      prev.last = isLast
    } else {
      items.push({ type: 'turn', messages: [m], last: isLast })
    }
  })
  return items
}

interface MessageListProps {
  messages: Message[]
  streaming: boolean
  streamingState: StreamingState
  /** Mid-run feedback queued but not yet injected into the agent loop. */
  pendingFeedback?: string[]
}

export default function MessageList({ messages, streaming, streamingState, pendingFeedback }: MessageListProps) {
  const { t } = useTranslation()
  const scrollRef = useRef<HTMLDivElement>(null)
  // "Following" = auto-scroll is on. It is switched off the moment the reader
  // scrolls up and back on when they return to the bottom (or use the pill).
  const followingRef = useRef(true)
  const [following, setFollowingState] = useState(true)
  const lastScrollTop = useRef(0)
  const lastScrollHeight = useRef(0)

  const setFollowing = useCallback((value: boolean) => {
    followingRef.current = value
    setFollowingState(value)
  }, [])

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
  }, [])

  const onScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    // Our own scrolls only ever move down, so an upward move is the reader's.
    // A shrinking thread also lowers scrollTop (the browser clamps it); that
    // is not the reader, hence the scrollHeight guard.
    const movedUp = el.scrollTop < lastScrollTop.current && el.scrollHeight >= lastScrollHeight.current
    lastScrollTop.current = el.scrollTop
    lastScrollHeight.current = el.scrollHeight
    if (movedUp) {
      setFollowing(false)
      return
    }
    // Not gated on the threshold alone: a smooth scroll in flight reports
    // positions far from the bottom, which must not stop the follow.
    if (el.scrollHeight - el.scrollTop - el.clientHeight < STICK_THRESHOLD) setFollowing(true)
  }, [setFollowing])

  const jumpToBottom = useCallback(() => {
    setFollowing(true)
    scrollToBottom('smooth')
  }, [setFollowing, scrollToBottom])

  const lastMsg = messages[messages.length - 1]
  const lastContent = lastMsg?.content[0]?.text ?? ''
  const thinkingLen = streamingState.thinking.length
  const toolCount = streamingState.toolCalls.length
  const itemCount = streamingState.items.length
  const lastItem = streamingState.items[itemCount - 1]
  const lastItemLen = lastItem && lastItem.type !== 'tool' ? lastItem.text.length : 0

  // Follow new content while following is on; a newly sent user message
  // always brings the view down and resumes the follow.
  const prevCount = useRef(messages.length)
  useEffect(() => {
    const sent = messages.length > prevCount.current && lastMsg?.role === 'user'
    prevCount.current = messages.length
    if (sent) setFollowing(true)
    if (followingRef.current) scrollToBottom('smooth')
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messages.length, lastContent, thinkingLen, toolCount, itemCount, lastItemLen, pendingFeedback?.length])

  // Opening a session lands on its latest message.
  const firstId = messages[0]?.id
  useLayoutEffect(() => {
    setFollowing(true)
    lastScrollTop.current = 0
    lastScrollHeight.current = 0
    scrollToBottom('auto')
  }, [firstId, scrollToBottom, setFollowing])

  const lastMessage = messages[messages.length - 1]
  // Only show LoadingBubble when there is no assistant message yet.
  // useChat always adds an empty assistant message before starting the SSE stream,
  // so MessageBubble handles all live state (spinner, thinking, tool calls).
  // Showing LoadingBubble when text === '' causes duplicate tool-call rows.
  const showLoadingBubble = streaming && (!lastMessage || lastMessage.role !== 'assistant')

  return (
    <div className="chat-viewport">
      <div ref={scrollRef} className="chat-scroll" onScroll={onScroll}>
        <div className="chat-column chat-thread">
          {groupTurns(messages).map((item) =>
            item.type === 'turn' ? (
              <AssistantTurn
                key={item.messages[0].id}
                messages={item.messages}
                streaming={streaming && item.last}
                streamingState={item.last ? streamingState : undefined}
              />
            ) : (
              <MessageBubble key={item.message.id} message={item.message} />
            ),
          )}

          {showLoadingBubble && <LoadingBubble streamingState={streamingState} />}

          {pendingFeedback && pendingFeedback.length > 0 && <QueuedFeedback items={pendingFeedback} />}
        </div>
      </div>

      {!following && (
        <div className="chat-scroll-bottom">
          <Button size="sm" icon={<ArrowDown size={14} />} onClick={jumpToBottom}>
            {t('chat.scrollToBottom')}
          </Button>
        </div>
      )}
    </div>
  )
}
