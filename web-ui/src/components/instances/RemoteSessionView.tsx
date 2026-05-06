import { useState, useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faComments, faStop, faPaperPlane, faSpinner, faCircle } from '@fortawesome/free-solid-svg-icons'
import { format } from 'date-fns'
import { useInstancesStore, type RemoteSession, type InstanceInfo } from '@/stores/instancesStore'
import api from '@/services/api'

interface StreamEvent {
  topic: string
  payload: Record<string, unknown>
}

interface RemoteSessionViewProps {
  instance: InstanceInfo
}

export default function RemoteSessionView({ instance }: RemoteSessionViewProps) {
  const { remoteSessions } = useInstancesStore()
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [streamEvents, setStreamEvents] = useState<StreamEvent[]>([])
  const [streamConnected, setStreamConnected] = useState(false)
  const [messageText, setMessageText] = useState('')
  const [sendingMessage, setSendingMessage] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [showMessageInput, setShowMessageInput] = useState(false)
  const streamRef = useRef<EventSource | null>(null)
  const eventsEndRef = useRef<HTMLDivElement | null>(null)

  const { sendRemoteMessage, cancelRemote } = useInstancesStore()

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

    const token = api.getToken()
    const baseURL = (window as Window & { __PANDO_API_BASE__?: string }).__PANDO_API_BASE__ || ''
    const url = `${baseURL}/api/v1/instances/${instance.instance_id}/sessions/${selectedSessionId}/stream${token ? `?token=${encodeURIComponent(token)}` : ''}`

    const es = new EventSource(url)
    streamRef.current = es

    es.onopen = () => setStreamConnected(true)

    es.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data as string) as StreamEvent
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

  // Auto-scroll to bottom
  useEffect(() => {
    eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [streamEvents])

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
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* Sessions list */}
      <div
        style={{
          width: 220,
          flexShrink: 0,
          borderRight: '1px solid var(--border)',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            padding: '0.625rem 1rem',
            borderBottom: '1px solid var(--border)',
            fontSize: 11,
            fontWeight: 600,
            color: 'var(--fg-dim)',
            textTransform: 'uppercase',
            letterSpacing: '0.05em',
          }}
        >
          Sessions ({remoteSessions.length})
        </div>
        <div style={{ flex: 1, overflow: 'auto' }}>
          {remoteSessions.length === 0 ? (
            <div
              style={{
                padding: '1.5rem 1rem',
                fontSize: 13,
                color: 'var(--fg-muted)',
                textAlign: 'center',
              }}
            >
              No sessions found
            </div>
          ) : (
            remoteSessions.map((session) => {
              const isSelected = session.id === selectedSessionId
              return (
                <button
                  key={session.id}
                  onClick={() => handleSelectSession(session)}
                  style={{
                    width: '100%',
                    background: isSelected ? 'var(--selected)' : 'transparent',
                    border: 'none',
                    borderBottom: '1px solid var(--border)',
                    padding: '0.625rem 1rem',
                    cursor: 'pointer',
                    textAlign: 'left',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '0.2rem',
                    borderLeft: isSelected ? '3px solid var(--primary)' : '3px solid transparent',
                  }}
                >
                  <div
                    style={{
                      fontSize: 12,
                      fontWeight: isSelected ? 600 : 400,
                      color: 'var(--fg)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {session.title || 'Untitled'}
                  </div>
                  <div style={{ fontSize: 10, color: 'var(--fg-muted)' }}>
                    {session.message_count} msgs · {format(new Date(session.updated_at), 'MMM d HH:mm')}
                  </div>
                </button>
              )
            })
          )}
        </div>
      </div>

      {/* Stream panel */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {!selectedSessionId ? (
          <div
            style={{
              flex: 1,
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--fg-muted)',
              gap: '0.75rem',
            }}
          >
            <FontAwesomeIcon icon={faComments} style={{ fontSize: 32, opacity: 0.3 }} />
            <p style={{ margin: 0, fontSize: 13 }}>Select a session to view its live stream</p>
          </div>
        ) : (
          <>
            {/* Session header */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                padding: '0.625rem 1rem',
                borderBottom: '1px solid var(--border)',
                flexShrink: 0,
                gap: '0.5rem',
                flexWrap: 'wrap',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: 0 }}>
                <FontAwesomeIcon
                  icon={faCircle}
                  style={{
                    fontSize: 7,
                    color: streamConnected ? '#4ade80' : 'var(--fg-dim)',
                    flexShrink: 0,
                  }}
                />
                <span
                  style={{
                    fontSize: 13,
                    fontWeight: 600,
                    color: 'var(--fg)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {selectedSession?.title || 'Untitled session'}
                </span>
                <span style={{ fontSize: 11, color: 'var(--fg-dim)', flexShrink: 0 }}>
                  {streamConnected ? 'live' : 'connecting…'}
                </span>
              </div>
              <div style={{ display: 'flex', gap: '0.5rem', flexShrink: 0 }}>
                <button
                  onClick={() => setShowMessageInput(!showMessageInput)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.375rem',
                    padding: '0.375rem 0.75rem',
                    borderRadius: 'var(--radius-sm)',
                    border: 'none',
                    background: 'var(--primary)',
                    color: 'white',
                    fontSize: 12,
                    fontWeight: 600,
                    cursor: 'pointer',
                    fontFamily: 'inherit',
                  }}
                >
                  <FontAwesomeIcon icon={faPaperPlane} style={{ fontSize: 10 }} />
                  Send Message
                </button>
                <button
                  onClick={() => void handleCancel()}
                  disabled={cancelling}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.375rem',
                    padding: '0.375rem 0.75rem',
                    borderRadius: 'var(--radius-sm)',
                    border: '1px solid #e55',
                    background: 'transparent',
                    color: '#e55',
                    fontSize: 12,
                    fontWeight: 600,
                    cursor: cancelling ? 'not-allowed' : 'pointer',
                    opacity: cancelling ? 0.6 : 1,
                    fontFamily: 'inherit',
                  }}
                >
                  {cancelling ? (
                    <FontAwesomeIcon icon={faSpinner} spin style={{ fontSize: 10 }} />
                  ) : (
                    <FontAwesomeIcon icon={faStop} style={{ fontSize: 10 }} />
                  )}
                  Cancel
                </button>
              </div>
            </div>

            {/* Send message input */}
            {showMessageInput && (
              <div
                style={{
                  display: 'flex',
                  alignItems: 'flex-end',
                  gap: '0.5rem',
                  padding: '0.625rem 1rem',
                  borderBottom: '1px solid var(--border)',
                  background: 'var(--sidebar-bg)',
                  flexShrink: 0,
                }}
              >
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
                  style={{
                    flex: 1,
                    padding: '0.5rem 0.75rem',
                    borderRadius: 'var(--radius-sm)',
                    border: '1px solid var(--border)',
                    background: 'var(--bg)',
                    color: 'var(--fg)',
                    fontSize: 13,
                    fontFamily: 'inherit',
                    resize: 'vertical',
                    minHeight: 60,
                  }}
                  autoFocus
                />
                <button
                  onClick={() => void handleSendMessage()}
                  disabled={sendingMessage || !messageText.trim()}
                  style={{
                    padding: '0.5rem 1rem',
                    borderRadius: 'var(--radius-sm)',
                    border: 'none',
                    background: 'var(--primary)',
                    color: 'white',
                    fontSize: 13,
                    fontWeight: 600,
                    cursor: sendingMessage || !messageText.trim() ? 'not-allowed' : 'pointer',
                    opacity: sendingMessage || !messageText.trim() ? 0.6 : 1,
                    fontFamily: 'inherit',
                    flexShrink: 0,
                  }}
                >
                  {sendingMessage ? <FontAwesomeIcon icon={faSpinner} spin /> : 'Send'}
                </button>
              </div>
            )}

            {/* Live stream events */}
            <div style={{ flex: 1, overflow: 'auto', padding: '0.5rem 0' }}>
              {streamEvents.length === 0 ? (
                <div
                  style={{
                    padding: '2rem 1rem',
                    textAlign: 'center',
                    color: 'var(--fg-dim)',
                    fontSize: 13,
                  }}
                >
                  Waiting for events…
                </div>
              ) : (
                streamEvents.map((event, idx) => (
                  <StreamEventRow key={idx} event={event} />
                ))
              )}
              <div ref={eventsEndRef} />
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function StreamEventRow({ event }: { event: StreamEvent }) {
  const topic = event.topic ?? 'unknown'
  const payload = event.payload ?? {}

  // Determine display content based on topic
  let content: string | null = null
  if (typeof payload.token === 'string') content = payload.token
  else if (typeof payload.content === 'string') content = payload.content

  const topicColor = (t: string) => {
    if (t.startsWith('llm.')) return 'var(--primary)'
    if (t.startsWith('tool.')) return '#f59e0b'
    if (t.startsWith('session.')) return '#4a9eff'
    if (t.startsWith('message.')) return '#34d399'
    if (t.startsWith('instance.')) return 'var(--fg-muted)'
    return 'var(--fg-dim)'
  }

  return (
    <div
      style={{
        display: 'flex',
        gap: '0.5rem',
        padding: '0.2rem 1rem',
        fontSize: 12,
        alignItems: 'baseline',
        borderBottom: '1px solid var(--border)',
      }}
    >
      <span
        style={{
          fontFamily: 'monospace',
          fontSize: 10,
          color: topicColor(topic),
          flexShrink: 0,
          minWidth: 100,
          fontWeight: 600,
        }}
      >
        {topic}
      </span>
      {content != null ? (
        <span style={{ color: 'var(--fg)', fontFamily: 'monospace', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
          {content}
        </span>
      ) : (
        <span style={{ color: 'var(--fg-dim)', fontFamily: 'monospace', fontSize: 11 }}>
          {JSON.stringify(payload)}
        </span>
      )}
    </div>
  )
}
