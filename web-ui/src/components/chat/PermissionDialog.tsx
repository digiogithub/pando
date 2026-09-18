import { useTranslation } from 'react-i18next'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import type { PermissionAction, PermissionRequest } from '@pando/client/types'

/** Action of a request to run a bash command once outside the host sandbox. */
const EXECUTE_UNSANDBOXED = 'execute_unsandboxed'

/** Command of a bash permission request, when present in its params. */
function commandOf(req: PermissionRequest): string | undefined {
  const params = req.params as { command?: unknown } | null | undefined
  return params && typeof params.command === 'string' ? params.command : undefined
}

/** Justification of a request (top-level field, or the bash params). */
function justificationOf(req: PermissionRequest): string | undefined {
  if (req.justification) return req.justification
  const params = req.params as { justification?: unknown } | null | undefined
  return params && typeof params.justification === 'string' && params.justification ? params.justification : undefined
}

/**
 * PermissionDialog surfaces pending tool permission prompts emitted by the agent
 * when the active session is not in auto-approve ("auto mode") state. It mirrors
 * the TUI permission dialog: Allow once / Allow for session / Deny.
 *
 * A sandbox escalation (action "execute_unsandboxed": the agent asks to run a
 * bash command once outside the host sandbox) is shown with a warning style,
 * the agent's justification and the scope an "allow for session" answer covers.
 *
 * Only the first pending request is shown at a time; once answered the next
 * (if any) takes its place.
 */
export default function PermissionDialog() {
  const { t } = useTranslation()
  const pending = useSessionStore((s) => s.pendingPermissions)
  const respond = useSessionStore((s) => s.respondPermission)

  const req = pending[0]
  if (!req) return null

  const act = (action: PermissionAction) => {
    void respond(req.id, req.session_id, action)
  }

  const escalation = req.action === EXECUTE_UNSANDBOXED
  const command = escalation ? commandOf(req) : undefined
  const justification = escalation ? justificationOf(req) : undefined
  const grantPrefix = req.grant_key?.startsWith('prefix:') ? req.grant_key.slice('prefix:'.length) : undefined
  const warningColor = 'var(--warning, #d97706)'

  const secondaryButton = {
    padding: '0.5rem 1rem',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border)',
    background: 'transparent',
    color: 'var(--fg)',
    fontSize: 13,
    cursor: 'pointer',
    fontFamily: 'inherit',
  } as const

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1100,
      }}
    >
      <div
        role="alertdialog"
        aria-label={escalation ? t('chat.permission.escalationTitle') : t('chat.permission.title')}
        style={{
          background: 'var(--card-bg)',
          border: escalation ? `2px solid ${warningColor}` : '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          padding: '1.5rem',
          width: escalation ? 520 : 460,
          maxWidth: '90vw',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
      >
        <h3
          style={{
            fontSize: 16,
            fontWeight: 700,
            color: escalation ? warningColor : 'var(--fg)',
            marginBottom: '0.75rem',
          }}
        >
          {escalation ? `⚠ ${t('chat.permission.escalationTitle')}` : t('chat.permission.title')}
        </h3>

        {escalation && (
          <div
            style={{
              fontSize: 13,
              lineHeight: 1.5,
              color: 'var(--fg)',
              background: 'color-mix(in srgb, var(--warning, #d97706) 12%, transparent)',
              border: `1px solid ${warningColor}`,
              borderRadius: 'var(--radius-sm)',
              padding: '0.5rem 0.75rem',
              marginBottom: '0.75rem',
            }}
          >
            {t('chat.permission.escalationWarning')}
          </div>
        )}

        <div style={{ fontSize: 13, color: 'var(--fg-muted)', lineHeight: 1.6, marginBottom: '1rem' }}>
          <div>
            <strong style={{ color: 'var(--fg)' }}>{t('chat.permission.tool')}:</strong>{' '}
            <code style={{ fontSize: 12 }}>{req.tool_name}</code>
          </div>
          {req.action && !escalation && (
            <div>
              <strong style={{ color: 'var(--fg)' }}>{t('chat.permission.action')}:</strong> {req.action}
            </div>
          )}
          {req.path && (
            <div style={{ wordBreak: 'break-all' }}>
              <strong style={{ color: 'var(--fg)' }}>{t('chat.permission.path')}:</strong>{' '}
              <code style={{ fontSize: 12 }}>{req.path}</code>
            </div>
          )}
          {escalation ? (
            <>
              {command && (
                <div style={{ marginTop: '0.5rem' }}>
                  <strong style={{ color: 'var(--fg)' }}>{t('chat.permission.command')}:</strong>
                  <pre
                    style={{
                      margin: '0.25rem 0 0',
                      padding: '0.5rem',
                      fontSize: 12,
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-all',
                      background: 'var(--bg)',
                      border: '1px solid var(--border)',
                      borderRadius: 'var(--radius-sm)',
                      color: 'var(--fg)',
                    }}
                  >
                    {command}
                  </pre>
                </div>
              )}
              {justification && (
                <div style={{ marginTop: '0.5rem', whiteSpace: 'pre-wrap' }}>
                  <strong style={{ color: 'var(--fg)' }}>{t('chat.permission.justification')}:</strong>{' '}
                  {justification}
                </div>
              )}
              <div style={{ marginTop: '0.5rem', fontSize: 12 }}>
                {grantPrefix
                  ? t('chat.permission.sessionScopePrefix', { prefix: grantPrefix })
                  : t('chat.permission.sessionScopeExact')}
              </div>
            </>
          ) : (
            req.description && (
              <div style={{ marginTop: '0.5rem', whiteSpace: 'pre-wrap' }}>{req.description}</div>
            )
          )}
        </div>

        {pending.length > 1 && (
          <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginBottom: '0.75rem' }}>
            {t('chat.permission.queued', { count: pending.length - 1 })}
          </div>
        )}

        <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', flexWrap: 'wrap' }}>
          <button onClick={() => act('deny')} style={secondaryButton}>
            {t('chat.permission.deny')}
          </button>
          <button onClick={() => act('allow_session')} style={secondaryButton}>
            {t('chat.permission.allowSession')}
          </button>
          <button
            onClick={() => act('allow')}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: escalation ? warningColor : 'var(--primary)',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            {escalation ? t('chat.permission.runOnce') : t('chat.permission.allow')}
          </button>
        </div>
      </div>
    </div>
  )
}
