import { useEffect, useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui'
import { CircleAlert, Plus } from '@/components/ui/icons'
import { useChat } from '@pando/client/hooks/useChat'
import { useGoal } from '@pando/client/hooks/useGoal'
import { useDesktopNotifications } from '@/hooks/useDesktopNotifications'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { shouldReattach } from '@pando/client/services/runSeq'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import { useFileChangesStore } from '@pando/client/stores/fileChangesStore'
import MessageList, { ChatEmptyHead, ChatSuggestions } from './MessageList'
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
  // Session just created by our own sendMessage (see onNewSession).
  const createdSessionRef = useRef<string | null>(null)

  const handleDone = useCallback((completed: boolean) => {
    if (!completed) return
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
      // The transcript of a session created by this very run is already on
      // screen (optimistic user + streaming assistant message): the load effect
      // below must not replace it with the server copy, which may not have the
      // messages persisted yet and would blank the chat until a reload.
      createdSessionRef.current = sessionId
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

  // The modified-files panel is fed by the live SSE stream, which only covers
  // the current run: rebuild it from the session history (plus agent-vcs) every
  // time a session's messages land, so a reload or a session switch keeps it.
  useEffect(() => {
    if (!activeSessionId || messages.length === 0) return
    void useFileChangesStore.getState().hydrateSession(activeSessionId, messages)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId, messages.length === 0])

  const activePlan = streamingState.plan.length > 0 ? streamingState.plan : persistentPlan

  // When the active session changes, load its messages. `is_running` comes from
  // the server (session detail here, then the pending poll below), and the effect
  // after this one turns it into a reconnection.
  useEffect(() => {
    if (!activeSessionId) return
    if (createdSessionRef.current === activeSessionId) {
      createdSessionRef.current = null
      return
    }
    void setActiveSession(activeSessionId)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId])

  // Reattach to a run this client has not shown. Driven by server state instead
  // of stream events: the pending poll reports `run_seq`, a counter of the
  // session's runs, and the client remembers the last one it streamed or loaded
  // (`seenRunSeq`). A greater value means a run started after what is on screen,
  // still running or already finished, e.g. an idle session resumed after a
  // delegated subagent. A dropped stream marks the baseline stale, which makes
  // the next poll reattach too. `/api/v1/sessions/{id}/stream` replays the
  // buffered run before going live, so nothing is lost; the stream's own `run`
  // event records the sequence, so the same run is never replayed twice.
  const serverRunSeq = sessions.find((s) => s.id === activeSessionId)?.run_seq
  const seenRunSeq = useSessionStore((s) => (activeSessionId ? s.seenRunSeq[activeSessionId] : undefined))
  useEffect(() => {
    if (!activeSessionId || serverRunSeq === undefined) return
    if (!shouldReattach({ serverSeq: serverRunSeq, seenSeq: seenRunSeq, streaming })) return
    // Claim the run before attaching so a second poll cannot attach again.
    useSessionStore.getState().setSeenRunSeq(activeSessionId, serverRunSeq)
    reconnectSession(activeSessionId)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSessionId, serverRunSeq, seenRunSeq, streaming])

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

  const isEmpty = messages.length === 0 && !streaming
  const composer = (
    <ChatInput
      onSend={sendMessage}
      streaming={streaming}
      onCancel={() => void cancelStreaming()}
      goalActive={(goal ?? streamingState.goal)?.status === 'running'}
    />
  )

  return (
    <div className="chat-root">
      <div className={isEmpty ? 'chat-pane chat-pane--empty' : 'chat-pane'}>
        {/* New session — visible only when the app sidebar is collapsed */}
        {!sidebarOpen && (
          <div className="chat-float chat-float--left">
            <Button
              variant="ghost"
              size="sm"
              icon={<Plus size={14} />}
              onClick={() => {
                useSessionStore.setState({ activeSessionId: null })
                setMessages([])
              }}
            >
              {t('nav.newSession')}
            </Button>
          </div>
        )}
        <div className="chat-column">
          <GoalStatus goal={goal ?? streamingState.goal} cancelling={cancelling} onCancel={() => void cancelGoal()} />
          {activePlan.length > 0 && <PlanView plan={activePlan} />}
        </div>

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

      <ChatInfoSidebar plan={activePlan} />
    </div>
  )
}
