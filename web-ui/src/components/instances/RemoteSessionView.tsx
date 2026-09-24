import { useState, useEffect, useRef } from 'react'
import { MessageSquare, CircleStop, SendHorizontal } from '@/components/ui/icons'
import { Button, Spinner } from '@/components/ui'
import { format } from 'date-fns'
import { useInstancesStore, type RemoteSession, type RemoteMessage, type InstanceInfo } from '@pando/client/stores/instancesStore'
import api from '@pando/client/services/api'

interface StreamEvent {
  topic: string
  payload: Record<string, unknown>
}

// Topics that carry no conversation value and would otherwise flood the view
// (heartbeat fires every 5s on every instance).
const NOISE_TOPICS = new Set(['instance.heartbeat', 'instance.ping'])

interface RemoteSessionViewProps {
  instance: InstanceInfo
}

export default function RemoteSessionView({ instance }: RemoteSessionViewProps) {
  const {
    remoteSessions,
    remoteSessionsTotal,
    remoteSessionsHasMore,
    remoteSessionsLoadingMore,
    loadMoreRemoteSessions,
  } = useInstancesStore()
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [streamEvents, setStreamEvents] = useState<StreamEvent[]>([])
  const [history, setHistory] = useState<RemoteMessage[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [streamConnected, setStreamConnected] = useState(false)
  const [messageText, setMessageText] = useState('')
  const [sendingMessage, setSendingMessage] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [showMessageInput, setShowMessageInput] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const streamRef = useRef<EventSource | null>(null)
  const eventsEndRef = useRef<HTMLDivElement | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const { sendRemoteMessage, cancelRemote, fetchRemoteMessages } = useInstancesStore()

  // Load the conversation history of the selected remote session.
  useEffect(() => {
    if (!selectedSessionId) {
      setHistory([])
      setHistoryError(null)
      return
    }
    let cancelled = false
    setHistoryLoading(true)
    setHistoryError(null)
    void fetchRemoteMessages(instance.instance_id, selectedSessionId)
      .then((msgs) => {
        if (!cancelled) setHistory(msgs)
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setHistory([])
          setHistoryError(err instanceof Error ? err.message : 'failed to load messages')
        }
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [selectedSessionId, instance.instance_id, fetchRemoteMessages])

  // Reconnect stream when session changes
  useEffect(() => {
    if (!selectedSessionId) return

    // Close existing stream
    if (streamRef.current) {
      streamRef.current.close()
      streamRef.current = null
    }
    setStreamEvents([])
    setStreamConnected(false)
    setAutoScroll(true)

    const token = api.getToken()
    const baseURL = (window as Window & { __PANDO_API_BASE__?: string }).__PANDO_API_BASE__ || ''
    const url = `${baseURL}/api/v1/instances/${instance.instance_id}/sessions/${selectedSessionId}/stream${token ? `?token=${encodeURIComponent(token)}` : ''}`

    const es = new EventSource(url)
    streamRef.current = es

    es.onopen = () => setStreamConnected(true)

    es.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data as string) as StreamEvent
        // Heartbeats only prove the stream is alive; they must not flood or
        // scroll the view.
        if (NOISE_TOPICS.has(event.topic)) return
        setStreamEvents((prev) => [...prev.slice(-200), event])
      } catch {
        // ignore parse errors
      }
    }

    es.onerror = () => {
      setStreamConnected(false)
    }

    return () => {
      es.close()
      streamRef.current = null
    }
  }, [selectedSessionId, instance.instance_id])

  // Auto-scroll to bottom only while the user is parked at the bottom. Scrolling
  // up disables it (and shows a "resume" badge) so reading old messages is not
  // interrupted by incoming events.
  useEffect(() => {
    if (!autoScroll) return
    eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [streamEvents, history, autoScroll])

  // Track whether the viewport is at the bottom; that drives autoScroll.
  const handleScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    setAutoScroll(atBottom)
  }

  const jumpToBottom = () => {
    setAutoScroll(true)
    eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  const handleSelectSession = (session: RemoteSession) => {
    setSelectedSessionId(session.id)
    setShowMessageInput(false)
    setMessageText('')
  }

  const handleSendMessage = async () => {
    if (!selectedSessionId || !messageText.trim()) return
    setSendingMessage(true)
    try {
      await sendRemoteMessage(instance.instance_id, selectedSessionId, messageText.trim())
      setMessageText('')
      setShowMessageInput(false)
    } catch {
      // error handled silently
    } finally {
      setSendingMessage(false)
    }
  }

  const handleCancel = async () => {
    if (!selectedSessionId) return
    setCancelling(true)
    try {
      await cancelRemote(instance.instance_id, selectedSessionId)
    } catch {
      // error handled silently
    } finally {
      setCancelling(false)
    }
  }

  const selectedSession = remoteSessions.find((s) => s.id === selectedSessionId)

  return (
    <div className="flex h-full overflow-hidden">
      {/* Sessions list */}
      <div className="split-pane-side" style={{ width: 220 }}>
        <div className="entity-row-group-header">
          Sessions ({remoteSessions.length}
          {remoteSessionsTotal > remoteSessions.length ? `/${remoteSessionsTotal}` : ''})
        </div>
        <div
          className="flex-1 overflow-auto"
          onScroll={(e) => {
            // Lazy-load the next page when scrolled near the end of the list.
            const el = e.currentTarget
            if (el.scrollHeight - el.scrollTop - el.clientHeight < 80) {
              void loadMoreRemoteSessions()
            }
          }}
        >
          {remoteSessions.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted">No sessions found</div>
          ) : (
            remoteSessions.map((session) => {
              const isSelected = session.id === selectedSessionId
              return (
                <button
                  key={session.id}
                  onClick={() => handleSelectSession(session)}
                  data-selected={isSelected || undefined}
                  className="entity-row w-full text-left"
                >
                  <div className={`is-ellipsis overflow-hidden whitespace-nowrap text-xs text-fg ${isSelected ? 'font-semibold' : ''}`}>
                    {session.title || 'Untitled'}
                  </div>
                  <div className="text-[10px] text-muted">
                    {session.message_count} msgs · {format(new Date(session.updated_at), 'MMM d HH:mm')}
                  </div>
                </button>
              )
            })
          )}
          {remoteSessionsHasMore && (
            <div className="p-2">
              <Button variant="secondary" size="sm" block loading={remoteSessionsLoadingMore} onClick={() => void loadMoreRemoteSessions()}>
                {remoteSessionsLoadingMore ? 'Loading…' : `Load more (${remoteSessions.length}/${remoteSessionsTotal})`}
              </Button>
            </div>
          )}
        </div>
      </div>

      {/* Stream panel */}
      <div className="flex flex-1 flex-col overflow-hidden">
        {!selectedSessionId ? (
          <div className="centered-fill">
            <MessageSquare size={30} className="text-faint opacity-70" />
            <p className="text-sm">Select a session to view its live stream</p>
          </div>
        ) : (
          <>
            {/* Session header */}
            <div className="flex flex-shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2.5">
              <div className="flex min-w-0 items-center gap-2">
                <span className={`status-dot ${streamConnected ? 'status-dot--success status-dot--pulse' : ''}`} />
                <span className="is-ellipsis overflow-hidden whitespace-nowrap text-sm font-semibold text-fg">
                  {selectedSession?.title || 'Untitled session'}
                </span>
                <span className="flex-shrink-0 text-xs text-faint">{streamConnected ? 'live' : 'connecting…'}</span>
              </div>
              <div className="flex flex-shrink-0 gap-2">
                <Button variant="primary" size="sm" icon={<SendHorizontal size={12} />} onClick={() => setShowMessageInput(!showMessageInput)}>
                  Send Message
                </Button>
                <Button
                  variant="danger"
                  size="sm"
                  icon={cancelling ? <Spinner size={12} /> : <CircleStop size={12} />}
                  disabled={cancelling}
                  onClick={() => void handleCancel()}
                >
                  Cancel
                </Button>
              </div>
            </div>

            {/* Send message input */}
            {showMessageInput && (
              <div className="flex flex-shrink-0 items-end gap-2 border-b border-border bg-shell px-4 py-2.5">
                <textarea
                  value={messageText}
                  onChange={(e) => setMessageText(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault()
                      void handleSendMessage()
                    }
                  }}
                  placeholder="Type a message… (Enter to send, Shift+Enter for newline)"
                  rows={3}
                  className="ui-textarea min-h-[60px] flex-1"
                  autoFocus
                />
                <Button variant="primary" loading={sendingMessage} disabled={!messageText.trim()} onClick={() => void handleSendMessage()} className="flex-shrink-0">
                  Send
                </Button>
              </div>
            )}

            {/* Conversation history + live stream events */}
            <div className="relative flex flex-1 overflow-hidden">
              <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-auto py-2">
                {historyLoading && (
                  <div className="flex items-center justify-center gap-2 p-4 text-sm text-faint">
                    <Spinner size={13} /> Loading conversation…
                  </div>
                )}
                {historyError && (
                  <div className="px-4 py-3 text-xs text-danger">Could not load conversation: {historyError}</div>
                )}
                {history.map((msg) => (
                  <MessageRow key={msg.id} message={msg} />
                ))}
                {history.length > 0 && (
                  <div className="entity-row-group-header">Live stream</div>
                )}
                {streamEvents.length === 0 ? (
                  <div className="p-8 text-center text-sm text-faint">Waiting for events…</div>
                ) : (
                  streamEvents.map((event, idx) => <StreamEventRow key={idx} event={event} />)
                )}
                <div ref={eventsEndRef} />
              </div>
              {!autoScroll && (
                <Button variant="primary" size="sm" onClick={jumpToBottom} className="absolute bottom-3 right-4 shadow-md">
                  ↓ Jump to latest
                </Button>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function MessageRow({ message }: { message: RemoteMessage }) {
  const isUser = message.role === 'user'
  return (
    <div className="flex flex-col gap-0.5 border-b border-border px-4 py-2">
      <div className="flex items-baseline gap-2">
        <span className={`text-[10px] font-bold uppercase tracking-wide ${isUser ? 'text-accent' : 'text-success'}`}>
          {message.role}
        </span>
        <span className="text-[10px] text-faint">
          {message.created_at ? format(new Date(message.created_at), 'MMM d HH:mm') : ''}
        </span>
      </div>
      <div className="whitespace-pre-wrap break-words text-sm text-fg">{message.content}</div>
    </div>
  )
}

const TOPIC_COLOR: Record<string, string> = {
  llm: 'text-accent',
  tool: 'text-warning',
  session: 'text-info',
  message: 'text-success',
  instance: 'text-muted',
}

function topicColorClass(topic: string): string {
  const prefix = topic.split('.')[0]
  return TOPIC_COLOR[prefix] ?? 'text-faint'
}

function StreamEventRow({ event }: { event: StreamEvent }) {
  const topic = event.topic ?? 'unknown'
  const payload = event.payload ?? {}

  // Determine display content based on topic
  let content: string | null = null
  if (typeof payload.token === 'string') content = payload.token
  else if (typeof payload.content === 'string') content = payload.content

  return (
    <div className="flex items-baseline gap-2 border-b border-border px-4 py-0.5 text-xs">
      <span className={`min-w-[100px] flex-shrink-0 font-mono text-[10px] font-semibold ${topicColorClass(topic)}`}>
        {topic}
      </span>
      {content != null ? (
        <span className="whitespace-pre-wrap break-all font-mono text-fg">{content}</span>
      ) : (
        <span className="font-mono text-[11px] text-faint">{JSON.stringify(payload)}</span>
      )}
    </div>
  )
}
