import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useChat } from '@pando/client/hooks/useChat'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useFileChangesStore } from '@pando/client/stores/fileChangesStore'
import MessageList, { ChatEmptyHead, ChatSuggestions } from './MessageList'
import ChatInput from './ChatInput'
import FileChangesBar from './FileChangesBar'
import ChatInfoSidebar from './ChatInfoSidebar'
import { Button } from '@/components/ui'
import { CircleAlert, Plus, SquareTerminal } from '@/components/ui/icons'

/**
 * Simple chat mode: the conversation alone. It renders inside MainLayout, whose
 * title bar and sidebar switch to their trimmed simple variants (sessions +
 * settings only) while the layout store's chatMode is 'simple'.
 */
export default function SimpleChatView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { messages, fetchSessions, sessions, activeSessionId, setMessages } = useSessionStore()
  const connected = useServerStore((s) => s.connected)
  const sidebarOpen = useLayoutStore((s) => s.sidebarOpen)
  const chatMode = useLayoutStore((s) => s.chatMode)
  const setChatMode = useLayoutStore((s) => s.setChatMode)
  const setQuickMenuOpen = useLayoutStore((s) => s.setQuickMenuOpen)
  const { sendMessage, streaming, error, cancelStreaming, streamingState, pendingFeedback } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
  })

  // Reaching this route (sidebar link, desktop --simple, bookmark) selects the
  // simple mode; it is persisted so the next launch opens here again.
  useEffect(() => {
    if (useLayoutStore.getState().chatMode !== 'simple') setChatMode('simple')
  }, [setChatMode])

  // Leaving the mode from elsewhere (title bar, server-side preference) moves
  // back to the full chat.
  const seenSimple = useRef(false)
  useEffect(() => {
    if (chatMode === 'simple') seenSimple.current = true
    else if (seenSimple.current) navigate('/', { replace: true })
  }, [chatMode, navigate])

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

  const totalTokens = activeSession
    ? activeSession.prompt_tokens + activeSession.completion_tokens
    : 0

  const newSession = () => {
    useSessionStore.setState({ activeSessionId: null })
    setMessages([])
  }
  const isEmpty = messages.length === 0 && !streaming
  const composer = <ChatInput onSend={sendMessage} streaming={streaming} onCancel={cancelStreaming} />

  return (
    <div className="chat-simple">
      <div className="chat-simple-body">
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
    </div>
  )
}
