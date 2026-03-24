import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircle } from '@fortawesome/free-solid-svg-icons'
import { useSessionStore } from '@/stores/sessionStore'
import { useServerStore } from '@/stores/serverStore'

export default function StatusBar() {
  const { activeSessionId, sessions } = useSessionStore()
  const connected = useServerStore((s) => s.connected)
  const activeSession = sessions.find((s) => s.id === activeSessionId)

  return (
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
        gap: '1rem',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        {activeSession && (
          <>
            <span>Session: <code style={{ fontSize: 10 }}>{activeSession.id.slice(0, 8)}…</code></span>
            <span>·</span>
            <span>{activeSession.message_count} messages</span>
            {(activeSession.prompt_tokens > 0 || activeSession.completion_tokens > 0) && (
              <>
                <span>·</span>
                <span>{(activeSession.prompt_tokens + activeSession.completion_tokens).toLocaleString()} tokens</span>
              </>
            )}
          </>
        )}
        {!activeSession && <span>No active session</span>}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <FontAwesomeIcon
          icon={faCircle}
          style={{
            fontSize: 7,
            color: connected ? 'var(--success)' : 'var(--error)',
          }}
        />
        <span>{connected ? 'Connected' : 'Disconnected'}</span>
      </div>
    </div>
  )
}
