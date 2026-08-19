import { useEffect, useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlus } from '@fortawesome/free-solid-svg-icons'
import { useChat } from '@/hooks/useChat'
import { useGoal } from '@/hooks/useGoal'
import { useDesktopNotifications } from '@/hooks/useDesktopNotifications'
import { useSessionStore } from '@/stores/sessionStore'
import { useLayoutStore } from '@/stores/layoutStore'
import { useFileChangesStore } from '@/stores/fileChangesStore'
import MessageList from './MessageList'
import ChatInput from './ChatInput'
import FileChangesBar from './FileChangesBar'
import GoalStatus from './GoalStatus'
import PlanView from './PlanView'
import ChatInfoSidebar from './ChatInfoSidebar'

export default function ChatView() {
  const { t } = useTranslation()
  const { messages, fetchSessions, sessions, activeSessionId, setMessages, setActiveSession } = useSessionStore()
  const { notify } = useDesktopNotifications()
  const sidebarOpen = useLayoutStore((s) => s.sidebarOpen)
  const { goal, applyGoalEvent, cancelGoal, cancelling } = useGoal(activeSessionId)
  // Sessions whose run really finished (a `done` event, not a dropped stream).
  // Guards against a poll response that was already in flight when the run ended
  // and would otherwise report the session as running and trigger a pointless
  // reattach — which replays the whole event buffer.
  const finishedSessionRef = useRef<string | null>(null)

  const handleDone = useCallback((completed: boolean) => {
    if (!completed) return
    if (activeSessionId) finishedSessionRef.current = activeSessionId
    const session = sessions.find((s) => s.id === activeSessionId)
    const title = session?.title ?? t('chat.agentDoneTitle')
    notify(title, {
      body: t('chat.agentDoneBody'),
      onClick: () => {
        window.focus()
      },
      onlyWhenBackground: true,
    })
  }, [notify, sessions, activeSessionId, t])

  const { sendMessage, reconnectSession, streaming, error, cancelStreaming, streamingState, pendingFeedback } = useChat({
    onNewSession: (sessionId) => {
      useSessionStore.setState({ activeSessionId: sessionId })
      fetchSessions()
    },
    onDone: handleDone,
    onEvent: applyGoalEvent,
    onCancelled: async (sessionId) => {
      if (!sessionId) return
      try {
        await cancelGoal()
      } catch {
        // useGoal exposes the error state; keep UI responsive here.
      }
    },
  })

  // Persist plan across stream completions so it stays visible after done
  const [persistentPlan, setPersistentPlan] = useState<{ title: string; status: string }[]>([])
  useEffect(() => {
    if (streamingState.plan.length > 0) {
      setPersistentPlan(streamingState.plan)
    }
  }, [streamingState.plan])
  useEffect(() => {
    setPersistentPlan([])
    useFileChangesStore.getState().clearChanges()
  }, [activeSessionId])

  const activePlan = streamingState.plan.length > 0 ? streamingState.plan : persistentPlan

  // Track which session we last reconnected to avoid duplicate connections. The
  // ref is re-armed whenever the server reports the session as no longer running,
  // so a later run (or a stream that dropped mid-run) can reattach again.
  const reconnectedSessionRef = useRef<string | null>(null)

  // When the active session changes, load its messages. `is_running` comes from
  // the server (session detail here, then the pending poll below), and the effect
  // after this one turns it into a reconnection.
  useEffect(() => {
    if (!activeSessionId) return
    void setActiveSession(activeSessionId)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId])

  // Reattach to a run the server still considers alive. Driven by server state
  // instead of by stream events: `handleDone` fires both on a real `done` and on
  // a dropped connection, so the client cannot tell them apart on its own.
  // `/api/v1/sessions/{id}/stream` replays the buffered events before going live,
  // so nothing is lost.
  const sessionRunning = Boolean(sessions.find((s) => s.id === activeSessionId)?.is_running)
  useEffect(() => {
    if (!sessionRunning) {
      reconnectedSessionRef.current = null
      return
    }
    if (!activeSessionId || streaming) return
    if (finishedSessionRef.current === activeSessionId) return
    if (reconnectedSessionRef.current === activeSessionId) return
    reconnectedSessionRef.current = activeSessionId
    reconnectSession(activeSessionId)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId, sessionRunning, streaming])

  // A new run for the session clears the "finished" guard so it can be reattached
  // again if its stream drops.
  useEffect(() => {
    if (streaming) finishedSessionRef.current = null
  }, [streaming])

  // Load sessions on mount if not already loaded
  useEffect(() => {
    void fetchSessions()
  }, [fetchSessions])

  // Poll for prompts blocking the session (AskUserQuestion / permissions). They
  // normally arrive over the SSE stream, but a run that continued in the
  // background — or a stream that dropped — would otherwise leave the dialog
  // unrendered while the agent stays blocked inside the tool.
  useEffect(() => {
    if (!activeSessionId) return
    const fetchPending = useSessionStore.getState().fetchPendingRequests
    void fetchPending(activeSessionId)
    const timer = window.setInterval(() => {
      void fetchPending(activeSessionId)
    }, 4000)
    return () => window.clearInterval(timer)
  }, [activeSessionId])

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden', position: 'relative' }}>
      <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative' }}>
      {/* New session FAB — visible only when sidebar is collapsed */}
      {!sidebarOpen && (
        <button
          title={t('nav.newSession')}
          onClick={() => {
            useSessionStore.setState({ activeSessionId: null })
            setMessages([])
          }}
          style={{
            position: 'absolute',
            top: '0.5rem',
            left: '0.5rem',
            zIndex: 10,
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            padding: '0.375rem 0.625rem',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            fontSize: 12,
            lineHeight: 1,
            boxShadow: '0 1px 4px rgba(0,0,0,0.15)',
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.color = 'var(--fg)'
            e.currentTarget.style.borderColor = 'var(--primary)'
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.color = 'var(--fg-muted)'
            e.currentTarget.style.borderColor = 'var(--border)'
          }}
        >
          <FontAwesomeIcon icon={faPlus} style={{ fontSize: 10 }} />
          {t('nav.newSession')}
        </button>
      )}
      <GoalStatus goal={goal ?? streamingState.goal} cancelling={cancelling} onCancel={() => void cancelGoal()} />
      {activePlan.length > 0 && <PlanView plan={activePlan} />}
      <MessageList messages={messages} streaming={streaming} streamingState={streamingState} pendingFeedback={pendingFeedback} />

      {error && (
        <div
          style={{
            margin: '0 1rem 0.5rem',
            padding: '0.5rem 0.75rem',
            background: 'var(--error)',
            color: 'white',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      <FileChangesBar />
      <ChatInput onSend={sendMessage} streaming={streaming} onCancel={() => void cancelStreaming()} goalActive={(goal ?? streamingState.goal)?.status === 'running'} />
      </div>

      <ChatInfoSidebar plan={activePlan} />
    </div>
  )
}
