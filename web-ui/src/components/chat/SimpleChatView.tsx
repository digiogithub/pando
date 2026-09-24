import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { format } from 'date-fns'
import { useChat } from '@pando/client/hooks/useChat'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useFileChangesStore } from '@pando/client/stores/fileChangesStore'
import { authenticate } from '@pando/client/services/auth'
import MessageList, { ChatEmptyHead, ChatSuggestions } from './MessageList'
import ChatInput from './ChatInput'
import FileChangesBar from './FileChangesBar'
import ChatInfoSidebar from './ChatInfoSidebar'
import ModelSwitcher from '@/components/overlays/ModelSwitcher'
import QuickMenu from '@/components/overlays/QuickMenu'
import NetworkErrorBanner from '@/components/shared/NetworkErrorBanner'
import { Button, IconButton } from '@/components/ui'
import {
  ChevronRight, CircleAlert, Columns2, PanelLeft, Plus, SquareTerminal,
} from '@/components/ui/icons'

export default function SimpleChatView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const {
    messages,
    fetchSessions,
    sessions,
    activeSessionId,
    setActiveSession,
    sessionsHasMore,
    sessionsLoadingMore,
    sessionsTotal,
    loadMoreSessions,
  } = useSessionStore()
  const { connected, startHealthCheck, setConnected } = useServerStore()
  const { fetchSettings } = useSettingsStore()
  const {
    modelSwitcherOpen,
    quickMenuOpen,
    setModelSwitcherOpen,
    setQuickMenuOpen,
    setChatMode,
  } = useLayoutStore()
  const { sendMessage, streaming, error, cancelStreaming, streamingState, pendingFeedback } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
  })

  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [sessionsOpen, setSessionsOpen] = useState(true)

  const activeSession = sessions.find((s) => s.id === activeSessionId)

  // Rebuild the modified-files panel from history (plus agent-vcs) whenever a
  // session is opened: the SSE stream only describes the current run.
  useEffect(() => {
    if (!activeSessionId) {
      useFileChangesStore.getState().clearChanges()
      return
    }
    if (messages.length === 0) return
    void useFileChangesStore.getState().hydrateSession(activeSessionId, messages)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId, messages.length === 0])

  // Initialize auth + health check on standalone mount
  useEffect(() => {
    void authenticate()
      .then(() => {
        fetchSessions()
        fetchSettings()
        setConnected(true)
      })
      .catch(() => setConnected(false))

    const stop = startHealthCheck()
    return stop
  }, [fetchSessions, fetchSettings, setConnected, startHealthCheck])

  const totalTokens = activeSession
    ? activeSession.prompt_tokens + activeSession.completion_tokens
    : 0


  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === 'p') {
        e.preventDefault()
        setQuickMenuOpen(true)
      }
      if (e.ctrlKey && e.key === 'o') {
        e.preventDefault()
        setModelSwitcherOpen(true)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [setModelSwitcherOpen, setQuickMenuOpen])

  const newSession = () => useSessionStore.setState({ activeSessionId: null, messages: [] })
  const isEmpty = messages.length === 0 && !streaming
  const composer = <ChatInput onSend={sendMessage} streaming={streaming} onCancel={cancelStreaming} />

  return (
    <div className="chat-simple">
      {/* Header */}
      <header className="chat-simple-header">
        <IconButton
          size="sm"
          aria-label={t('chat.simple.toggleSessions')}
          tooltip
          active={sidebarOpen}
          icon={<PanelLeft size={16} />}
          onClick={() => setSidebarOpen((v) => !v)}
        />
        <span className="chat-simple-brand" aria-label="Pando">
          <img src="/pando-icon.svg" alt="" />
          Pando
        </span>

        {activeSession && (
          <>
            <span className="chat-simple-sep">/</span>
            <span className="chat-simple-title">
              {activeSession.title || t('chat.simple.sessionFallback', { id: activeSession.id.slice(0, 8) })}
            </span>
          </>
        )}

        <span className="chat-spacer" />

        {/* Switch to the advanced view */}
        <IconButton
          size="sm"
          aria-label={t('chat.simple.advanced')}
          tooltip
          icon={<Columns2 size={16} />}
          onClick={() => { setChatMode('advanced'); navigate('/') }}
        />
      </header>

      <NetworkErrorBanner />

      {/* Body: sidebar + chat */}
      <div className="chat-simple-body">
        {/* Sessions sidebar */}
        {sidebarOpen && (
          <aside className="chat-simple-sessions">
            <div className="chat-simple-sessions-head">
              <button
                type="button"
                className="chat-simple-sessions-toggle"
                aria-expanded={sessionsOpen}
                onClick={() => setSessionsOpen((v) => !v)}
              >
                <ChevronRight size={14} className="chat-chevron" />
                {t('chat.simple.sessions')}
              </button>
              <IconButton
                size="sm"
                aria-label={t('nav.newSession')}
                tooltip
                icon={<Plus size={16} />}
                onClick={newSession}
              />
            </div>

            {/* Session list */}
            {sessionsOpen && (
              <div
                className="chat-simple-list"
                onScroll={(e) => {
                  // Lazy-load the next page when the list is scrolled near its end.
                  const el = e.currentTarget
                  if (el.scrollHeight - el.scrollTop - el.clientHeight < 80) {
                    void loadMoreSessions()
                  }
                }}
              >
                {sessions.map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    className="chat-simple-session"
                    aria-current={s.id === activeSessionId}
                    title={s.prompt_preview || s.title || t('chat.simple.untitled')}
                    onClick={() => setActiveSession(s.id)}
                  >
                    <span className="chat-simple-session-title">{s.title || t('chat.simple.untitled')}</span>
                    <span className="chat-simple-session-meta">
                      {t('chat.simple.msgs', { count: s.message_count })} · {format(new Date(s.updated_at), 'MMM d')}
                    </span>
                  </button>
                ))}
                {sessionsHasMore && (
                  <Button size="sm" variant="ghost" block loading={sessionsLoadingMore} onClick={() => void loadMoreSessions()}>
                    {sessionsLoadingMore
                      ? t('common.loading')
                      : t('chat.simple.loadMore', { loaded: sessions.length, total: sessionsTotal })}
                  </Button>
                )}
                {sessions.length === 0 && <div className="chat-simple-empty">{t('chat.simple.noSessions')}</div>}
              </div>
            )}
          </aside>
        )}

        {/* Main chat area */}
        <div className={isEmpty ? 'chat-pane chat-pane--empty' : 'chat-pane'}>
          {/* New session — visible only when the sessions panel is collapsed */}
          {!sidebarOpen && (
            <div className="chat-float chat-float--left">
              <Button variant="ghost" size="sm" icon={<Plus size={14} />} onClick={newSession}>
                {t('nav.newSession')}
              </Button>
            </div>
          )}

          {isEmpty ? <ChatEmptyHead /> : (
          <MessageList messages={messages} streaming={streaming} streamingState={streamingState} pendingFeedback={pendingFeedback} />
        )}
        <div className="chat-column">
          {error && (
            <div className="chat-error" role="alert">
              <CircleAlert size={14} />
              <span>{error}</span>
            </div>
          )}
          {!isEmpty && <FileChangesBar />}
        </div>
        {composer}
        {isEmpty && <ChatSuggestions />}
        </div>

        <ChatInfoSidebar plan={streamingState.plan} />
      </div>

      {/* Footer status bar */}
      <footer className="chat-simple-footer">
        <div>
          <button type="button" className="chat-simple-link" onClick={() => setQuickMenuOpen(true)} title={t('chat.simple.openCommands')}>
            <SquareTerminal size={13} />
            <span>{t('chat.simple.commands')}</span>
            <span className="chat-simple-hide-mobile">Ctrl+P</span>
          </button>
          <span className="chat-meta-item">
            <span className={connected ? 'chat-conn-dot chat-conn-dot--on' : 'chat-conn-dot'} />
            {connected ? t('common.connected') : t('common.disconnected')}
          </span>
        </div>

        <div>
          {activeSession && totalTokens > 0 && (
            <span className="chat-meta-item">
              {activeSession.message_count} {t('common.messages')} · {totalTokens.toLocaleString()} {t('common.tokens')}
            </span>
          )}
        </div>
      </footer>

      {/* Overlays */}
      {quickMenuOpen && <QuickMenu />}
      {modelSwitcherOpen && <ModelSwitcher />}
    </div>
  )
}
