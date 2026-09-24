import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { useServerStore } from '@pando/client/stores/serverStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import ExternalAccessToggle from './ExternalAccessToggle'
import ExtensionSlot from '@/components/extensions/ExtensionSlot'
import MemorySyncIndicator from '@/components/extensions/MemorySyncIndicator'
import { Cpu, FastForward } from '@/components/ui/icons'

export default function StatusBar() {
  const { t } = useTranslation()
  const { activeSessionId, sessions } = useSessionStore()
  const autoApprove = useSessionStore((s) => s.autoApprove)
  const toggleAutoApprove = useSessionStore((s) => s.toggleAutoApprove)
  const connected = useServerStore((s) => s.connected)
  const activeSession = sessions.find((s) => s.id === activeSessionId)
  const defaultModel = useSettingsStore((s) => s.config.default_model)
  const setModelSwitcherOpen = useLayoutStore((s) => s.setModelSwitcherOpen)
  // On chat routes the composer's model chip already shows (and switches) the
  // model, so the status bar does not repeat it.
  const { pathname } = useLocation()
  const onChatRoute = pathname === '/' || pathname.startsWith('/chat')

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
    <footer className="shell-statusbar">
      <div className="shell-status-group">
        {activeSession && (
          <>
            <span className="shell-status-hide-mobile">
              {t('common.session')}: <code className="shell-status-code">{activeSession.id.slice(0, 8)}…</code>
            </span>
            <span className="shell-status-sep shell-status-hide-mobile" aria-hidden="true">·</span>
            <span>{activeSession.message_count} {t('common.messages')}</span>
            {(activeSession.prompt_tokens > 0 || activeSession.completion_tokens > 0) && (
              <>
                <span className="shell-status-sep" aria-hidden="true">·</span>
                <span
                  title={activeSession.tokens_estimated ? t('common.estimatedTokens', 'Estimated (updates live while the agent runs)') : undefined}
                  className={activeSession.tokens_estimated ? 'shell-status-estimated' : undefined}
                >
                  {activeSession.tokens_estimated ? '~' : ''}
                  {(activeSession.prompt_tokens + activeSession.completion_tokens).toLocaleString()} {t('common.tokens')}
                </span>
              </>
            )}
            {activeSession.cost > 0 && (
              <>
                <span className="shell-status-sep shell-status-hide-mobile" aria-hidden="true">·</span>
                <span
                  className="shell-status-hide-mobile"
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

      <div className="shell-status-group">
        {/* Auto-approve ("auto mode") toggle — Shift+Tab also toggles it */}
        {activeSessionId && (
          <button
            type="button"
            className={`shell-status-btn${autoApprove ? ' is-warning' : ''}`}
            onClick={() => { void toggleAutoApprove(activeSessionId) }}
            title={t('common.toggleAutoApprove', 'Toggle auto-approve (Shift+Tab)')}
            aria-pressed={autoApprove}
          >
            {autoApprove && <FastForward size={12} />}
            <span>{autoApprove ? t('shell.autoAcceptOn', 'auto-accept') : t('shell.autoAcceptOff', 'auto-accept off')}</span>
          </button>
        )}

        {/* External access (0.0.0.0 bind) toggle */}
        <span className="shell-status-hide-mobile">
          <ExternalAccessToggle />
        </span>

        {/* Model selector button (hidden where the composer shows it) */}
        {!onChatRoute && (
          <button
            type="button"
            className="shell-status-btn"
            onClick={() => setModelSwitcherOpen(true)}
            title={t('common.clickToSwitchModel')}
          >
            <Cpu size={12} />
            <span>{modelLabel}</span>
          </button>
        )}

        <span className="shell-status-conn" title={connected ? t('common.connected') : t('common.disconnected')}>
          <span className={`shell-dot ${connected ? 'is-ok' : 'is-danger'}`} aria-hidden="true" />
          <span className="shell-status-hide-mobile">{connected ? t('common.connected') : t('common.disconnected')}</span>
        </span>

        {/* Renders only when remembrance writes are leaving this machine. */}
        <MemorySyncIndicator />

        <ExtensionSlot slot="status-bar" />
      </div>
    </footer>
  )
}
