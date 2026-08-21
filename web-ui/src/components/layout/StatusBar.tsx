import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCircle, faMicrochip } from '@fortawesome/free-solid-svg-icons'
import { useTranslation } from 'react-i18next'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import ExternalAccessToggle from './ExternalAccessToggle'
import ExtensionSlot from '@/components/extensions/ExtensionSlot'

export default function StatusBar() {
  const { t } = useTranslation()
  const { activeSessionId, sessions } = useSessionStore()
  const autoApprove = useSessionStore((s) => s.autoApprove)
  const toggleAutoApprove = useSessionStore((s) => s.toggleAutoApprove)
  const connected = useServerStore((s) => s.connected)
  const activeSession = sessions.find((s) => s.id === activeSessionId)
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const setModelSwitcherOpen = useLayoutStore((s) => s.setModelSwitcherOpen)

  // Format model name: "claude-sonnet-4-6" → "Claude Sonnet 4.6", "copilot.gpt-4o" → "Copilot GPT-4o"
  const formatModel = (id: string): string => {
    if (id.startsWith('copilot.')) return 'Copilot ' + formatModel(id.slice(8))
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
            <span>{t('common.session')}: <code style={{ fontSize: 10 }}>{activeSession.id.slice(0, 8)}…</code></span>
            <span>·</span>
            <span>{activeSession.message_count} {t('common.messages')}</span>
            {(activeSession.prompt_tokens > 0 || activeSession.completion_tokens > 0) && (
              <>
                <span>·</span>
                <span
                  title={activeSession.tokens_estimated ? t('common.estimatedTokens', 'Estimated (updates live while the agent runs)') : undefined}
                  style={activeSession.tokens_estimated ? { opacity: 0.6, fontStyle: 'italic' } : undefined}
                >
                  {activeSession.tokens_estimated ? '~' : ''}
                  {(activeSession.prompt_tokens + activeSession.completion_tokens).toLocaleString()} {t('common.tokens')}
                </span>
              </>
            )}
            {activeSession.cost > 0 && (
              <>
                <span>·</span>
                <span
                  title={
                    activeSession.cache_read_tokens || activeSession.reasoning_tokens
                      ? t(
                          'common.usageBreakdown',
                          'Cache read: {{cacheRead}} · Cache write: {{cacheWrite}} · Reasoning: {{reasoning}}',
                          {
                            cacheRead: (activeSession.cache_read_tokens ?? 0).toLocaleString(),
                            cacheWrite: (activeSession.cache_creation_tokens ?? 0).toLocaleString(),
                            reasoning: (activeSession.reasoning_tokens ?? 0).toLocaleString(),
                          },
                        )
                      : undefined
                  }
                >
                  ${activeSession.cost.toFixed(4)}
                </span>
              </>
            )}
          </>
        )}
        {!activeSession && <span>{t('common.noActiveSession')}</span>}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        {/* Auto-approve ("auto mode") toggle — Shift+Tab also toggles it */}
        {activeSessionId && (
          <button
            onClick={() => { void toggleAutoApprove(activeSessionId) }}
            title={t('common.toggleAutoApprove', 'Toggle auto-approve (Shift+Tab)')}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.375rem',
              background: autoApprove ? 'var(--warning, #d97706)' : 'none',
              border: 'none',
              cursor: 'pointer',
              color: autoApprove ? '#fff' : 'var(--fg-muted)',
              fontSize: 11,
              fontWeight: autoApprove ? 600 : 400,
              padding: '0 0.5rem',
              height: 18,
              borderRadius: 'var(--radius-sm)',
              transition: 'color 0.15s, background 0.15s',
            }}
          >
            <span>{autoApprove ? '⏵⏵ auto-accept' : 'auto-accept off'}</span>
          </button>
        )}

        {/* External access (0.0.0.0 bind) toggle */}
        <ExternalAccessToggle />

        {/* Model selector button */}
        <button
          onClick={() => setModelSwitcherOpen(true)}
          title={t('common.clickToSwitchModel')}
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
        <span>{connected ? t('common.connected') : t('common.disconnected')}</span>

        <ExtensionSlot slot="status-bar" />
      </div>
    </div>
  )
}
