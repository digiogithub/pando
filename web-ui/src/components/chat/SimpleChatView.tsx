import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faCircle, faColumns, faChevronDown, faChevronRight, faPlus, faMicrochip,
} from '@fortawesome/free-solid-svg-icons'
import { format } from 'date-fns'
import { useChat } from '@/hooks/useChat'
import { useSessionStore } from '@/stores/sessionStore'
import { useServerStore } from '@/stores/serverStore'
import { useSettingsStore } from '@/stores/settingsStore'
import { useLayoutStore } from '@/stores/layoutStore'
import { authenticate } from '@/services/auth'
import MessageList from './MessageList'
import ChatInput from './ChatInput'
import ModelSwitcher from '@/components/overlays/ModelSwitcher'

export default function SimpleChatView() {
  const navigate = useNavigate()
  const { messages, fetchSessions, sessions, activeSessionId, setActiveSession } = useSessionStore()
  const { connected, startHealthCheck, setConnected } = useServerStore()
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const { modelSwitcherOpen, setModelSwitcherOpen } = useLayoutStore()
  const { sendMessage, streaming, error, cancelStreaming } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
  })

  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [sessionsOpen, setSessionsOpen] = useState(true)

  const activeSession = sessions.find((s) => s.id === activeSessionId)

  // Initialize auth + health check on standalone mount
  useEffect(() => {
    authenticate()
      .then(() => {
        fetchSessions()
        setConnected(true)
      })
      .catch(() => setConnected(false))

    const stop = startHealthCheck()
    return stop
  }, [])

  const totalTokens = activeSession
    ? activeSession.prompt_tokens + activeSession.completion_tokens
    : 0

  const formatModel = (id: string) => {
    if (id.startsWith('claude-')) {
      const rest = id.slice(7)
      const dash = rest.indexOf('-')
      if (dash === -1) return 'Claude ' + rest.charAt(0).toUpperCase() + rest.slice(1)
      const name = rest.slice(0, dash)
      const version = rest.slice(dash + 1).replace(/-/g, '.')
      return 'Claude ' + name.charAt(0).toUpperCase() + name.slice(1) + ' ' + version
    }
    if (id.startsWith('gpt-')) return 'GPT-' + id.slice(4)
    if (id.startsWith('gemini-')) return 'Gemini ' + id.slice(7)
    return id
  }
  const modelLabel = formatModel(defaultModel)

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100vh',
        background: 'var(--bg)',
        color: 'var(--fg)',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          padding: '0 1rem',
          height: 48,
          background: 'var(--sidebar-bg)',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
        }}
      >
        {/* Toggle sidebar */}
        <button
          onClick={() => setSidebarOpen((v) => !v)}
          title="Toggle sessions panel"
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            padding: '0.25rem',
            display: 'flex',
            alignItems: 'center',
          }}
        >
          <FontAwesomeIcon icon={faChevronRight} style={{ fontSize: 12, transform: sidebarOpen ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }} />
        </button>

        {/* Logo unificado igual que Sidebar/Header */}
        <img src="/pando-favicon.svg" alt="Pando" style={{ width: 20, height: 20 }} />
        <span style={{ fontWeight: 700, fontSize: 15, color: 'var(--primary)' }}>Pando</span>

        {activeSession && (
          <>
            <span style={{ color: 'var(--border)', fontSize: 16, userSelect: 'none' }}>·</span>
            <span
              style={{
                fontSize: 13,
                color: 'var(--fg-muted)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                maxWidth: 260,
              }}
            >
              {activeSession.title || `Session ${activeSession.id.slice(0, 8)}`}
            </span>
          </>
        )}

        <div style={{ flex: 1 }} />

        {/* Botón para ir a vista avanzada */}
        <button
          onClick={() => navigate('/')}
          title="Switch to advanced view"
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            padding: '0.25rem 0.625rem',
            background: 'var(--selected)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            cursor: 'pointer',
            color: 'var(--primary)',
            fontSize: 12,
            fontWeight: 500,
          }}
        >
          <FontAwesomeIcon icon={faColumns} style={{ fontSize: 11 }} />
          Advanced view
        </button>
      </div>

      {/* Body: sidebar + chat */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'flex' }}>

        {/* Sessions sidebar */}
        {sidebarOpen && (
          <aside
            style={{
              width: 220,
              flexShrink: 0,
              display: 'flex',
              flexDirection: 'column',
              background: 'var(--sidebar-bg)',
              borderRight: '1px solid var(--border)',
              overflow: 'hidden',
            }}
          >
            {/* Sessions header */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '0.375rem 0.75rem',
                cursor: 'pointer',
                userSelect: 'none',
              }}
              onClick={() => setSessionsOpen((v) => !v)}
            >
              <FontAwesomeIcon
                icon={sessionsOpen ? faChevronDown : faChevronRight}
                style={{ fontSize: 9, color: 'var(--fg-dim)', marginRight: '0.375rem' }}
              />
              <span style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-dim)', textTransform: 'uppercase', letterSpacing: '0.05em', flex: 1 }}>
                Sessions
              </span>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  useSessionStore.setState({ activeSessionId: null, messages: [] })
                }}
                title="New session"
                style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--fg-muted)', padding: '0 2px' }}
              >
                <FontAwesomeIcon icon={faPlus} style={{ fontSize: 11 }} />
              </button>
            </div>

            {/* Session list */}
            {sessionsOpen && (
              <div style={{ flex: 1, overflowY: 'auto', padding: '0.25rem 0' }}>
                {sessions.slice(0, 30).map((s) => (
                  <button
                    key={s.id}
                    onClick={() => setActiveSession(s.id)}
                    style={{
                      width: 'calc(100% - 1rem)',
                      background: s.id === activeSessionId ? 'var(--selected)' : 'transparent',
                      border: 'none',
                      borderRadius: 'var(--radius-sm)',
                      margin: '1px 0.5rem',
                      padding: '0.375rem 0.5rem',
                      cursor: 'pointer',
                      textAlign: 'left',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '0.5rem',
                    }}
                  >
                    <FontAwesomeIcon
                      icon={faCircle}
                      style={{ fontSize: 6, color: s.id === activeSessionId ? 'var(--primary)' : 'var(--fg-dim)', flexShrink: 0 }}
                    />
                    <div style={{ flex: 1, overflow: 'hidden' }}>
                      <div style={{ fontSize: 12, fontWeight: 500, color: 'var(--fg)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {s.title || 'Untitled session'}
                      </div>
                      <div style={{ fontSize: 10, color: 'var(--fg-muted)' }}>
                        {s.message_count} msgs · {format(new Date(s.updated_at), 'MMM d')}
                      </div>
                    </div>
                  </button>
                ))}
                {sessions.length === 0 && (
                  <div style={{ padding: '0.5rem 1rem', fontSize: 12, color: 'var(--fg-dim)' }}>
                    No sessions yet
                  </div>
                )}
              </div>
            )}
          </aside>
        )}

        {/* Main chat area */}
        <div style={{ flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          <div
            style={{
              flex: 1,
              overflow: 'hidden',
              display: 'flex',
              flexDirection: 'column',
              maxWidth: 900,
              width: '100%',
              margin: '0 auto',
              alignSelf: 'stretch',
            }}
          >
            <MessageList messages={messages} streaming={streaming} />
          </div>

          {/* Error banner */}
          {error && (
            <div
              style={{
                maxWidth: 900,
                width: '100%',
                margin: '0 auto',
                padding: '0 1rem',
              }}
            >
              <div
                style={{
                  marginBottom: '0.5rem',
                  padding: '0.5rem 0.75rem',
                  background: 'var(--error)',
                  color: 'white',
                  borderRadius: 'var(--radius-sm)',
                  fontSize: 13,
                }}
              >
                {error}
              </div>
            </div>
          )}

          {/* Input */}
          <div style={{ maxWidth: 900, width: '100%', margin: '0 auto' }}>
            <ChatInput onSend={sendMessage} streaming={streaming} onCancel={cancelStreaming} />
          </div>
        </div>
      </div>

      {/* Footer status bar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          height: 28,
          padding: '0 1rem',
          background: 'var(--bg-secondary)',
          borderTop: '1px solid var(--border)',
          fontSize: 11,
          color: 'var(--fg-muted)',
          flexShrink: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <FontAwesomeIcon
            icon={faCircle}
            style={{ fontSize: 7, color: connected ? 'var(--success)' : 'var(--error)' }}
          />
          <span>{connected ? 'Connected' : 'Disconnected'}</span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          {activeSession && totalTokens > 0 && (
            <>
              <span>{activeSession.message_count} messages</span>
              <span>·</span>
              <span>{totalTokens.toLocaleString()} tokens</span>
              <span style={{ opacity: 0.4 }}>·</span>
            </>
          )}
          {/* Model selector */}
          <button
            onClick={() => setModelSwitcherOpen(true)}
            title="Click to switch model"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.375rem',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              color: 'var(--fg-muted)',
              fontSize: 11,
              padding: '0 0.25rem',
              borderRadius: 'var(--radius-sm)',
              transition: 'color 0.15s',
            }}
            onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--primary)')}
            onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--fg-muted)')}
          >
            <FontAwesomeIcon icon={faMicrochip} style={{ fontSize: 10 }} />
            <span>{modelLabel}</span>
          </button>
        </div>
      </div>

      {/* Model switcher overlay */}
      {modelSwitcherOpen && <ModelSwitcher />}
    </div>
  )
}
