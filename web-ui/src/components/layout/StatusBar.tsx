import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircle, faMicrochip } from '@fortawesome/free-solid-svg-icons'
import { useSessionStore } from '@/stores/sessionStore'
import { useServerStore } from '@/stores/serverStore'
import { useSettingsStore } from '@/stores/settingsStore'
import { useLayoutStore } from '@/stores/layoutStore'

export default function StatusBar() {
  const { activeSessionId, sessions } = useSessionStore()
  const connected = useServerStore((s) => s.connected)
  const activeSession = sessions.find((s) => s.id === activeSessionId)
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const setModelSwitcherOpen = useLayoutStore((s) => s.setModelSwitcherOpen)

  // Format model name for display: shorten common prefixes
  const modelLabel = defaultModel
    .replace('claude-', 'Claude ')
    .replace('gpt-', 'GPT-')
    .replace('gemini-', 'Gemini ')

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
        {/* Model selector button */}
        <button
          onClick={() => setModelSwitcherOpen(true)}
          title="Click to switch model (Ctrl+O)"
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

        <span style={{ opacity: 0.4 }}>·</span>

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
