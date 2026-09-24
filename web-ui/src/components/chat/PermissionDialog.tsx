import { useTranslation } from 'react-i18next'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import type { PermissionAction, PermissionRequest } from '@pando/client/types'
import { Button, Dialog } from '@/components/ui'
import { ShieldCheck, TriangleAlert } from '@/components/ui/icons'

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
  const title = escalation ? (
    <span className="chat-dialog-title chat-dialog-title--warning">
      <TriangleAlert size={18} />
      {t('chat.permission.escalationTitle')}
    </span>
  ) : (
    <span className="chat-dialog-title">
      <ShieldCheck size={18} />
      {t('chat.permission.title')}
    </span>
  )

  return (
    <Dialog
      open
      // The agent is blocked until the user answers: no dismiss by scrim or Esc.
      onClose={() => {}}
      dismissible={false}
      hideClose
      size="md"
      title={title}
      footer={
        <>
          <Button onClick={() => act('deny')}>{t('chat.permission.deny')}</Button>
          <Button onClick={() => act('allow_session')}>{t('chat.permission.allowSession')}</Button>
          <Button variant={escalation ? 'danger' : 'primary'} onClick={() => act('allow')} data-autofocus>
            {escalation ? t('chat.permission.runOnce') : t('chat.permission.allow')}
          </Button>
        </>
      }
    >
      <div className="chat-perm">
        {escalation && <div className="chat-callout chat-callout--warning">{t('chat.permission.escalationWarning')}</div>}

        <dl className="chat-perm-grid">
          <dt>{t('chat.permission.tool')}</dt>
          <dd><code>{req.tool_name}</code></dd>
          {req.action && !escalation && (
            <>
              <dt>{t('chat.permission.action')}</dt>
              <dd>{req.action}</dd>
            </>
          )}
          {req.path && (
            <>
              <dt>{t('chat.permission.path')}</dt>
              <dd><code>{req.path}</code></dd>
            </>
          )}
          {escalation && justification && (
            <>
              <dt>{t('chat.permission.justification')}</dt>
              <dd className="chat-perm-desc">{justification}</dd>
            </>
          )}
        </dl>

        {escalation ? (
          <>
            {command && (
              <div className="chat-tool-block">
                <div className="chat-tool-label">{t('chat.permission.command')}</div>
                <pre className="chat-tool-pre"><span className="chat-tool-prompt">$ </span>{command}</pre>
              </div>
            )}
            <div className="chat-perm-note">
              {grantPrefix
                ? t('chat.permission.sessionScopePrefix', { prefix: grantPrefix })
                : t('chat.permission.sessionScopeExact')}
            </div>
          </>
        ) : (
          req.description && <div className="chat-perm-desc">{req.description}</div>
        )}

        {pending.length > 1 && (
          <div className="chat-perm-note">{t('chat.permission.queued', { count: pending.length - 1 })}</div>
        )}
      </div>
    </Dialog>
  )
}
