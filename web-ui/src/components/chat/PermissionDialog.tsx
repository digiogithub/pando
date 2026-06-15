import { useSessionStore } from '@/stores/sessionStore'
import type { PermissionAction } from '@/types'

/**
 * PermissionDialog surfaces pending tool permission prompts emitted by the agent
 * when the active session is not in auto-approve ("auto mode") state. It mirrors
 * the TUI permission dialog: Allow once / Allow for session / Deny.
 *
 * Only the first pending request is shown at a time; once answered the next
 * (if any) takes its place.
 */
export default function PermissionDialog() {
  const pending = useSessionStore((s) => s.pendingPermissions)
  const respond = useSessionStore((s) => s.respondPermission)

  const req = pending[0]
  if (!req) return null

  const act = (action: PermissionAction) => {
    void respond(req.id, req.session_id, action)
  }

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
        style={{
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          padding: '1.5rem',
          width: 460,
          maxWidth: '90vw',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
      >
        <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '0.75rem' }}>
          Permiso requerido
        </h3>

        <div style={{ fontSize: 13, color: 'var(--fg-muted)', lineHeight: 1.6, marginBottom: '1rem' }}>
          <div>
            <strong style={{ color: 'var(--fg)' }}>Herramienta:</strong>{' '}
            <code style={{ fontSize: 12 }}>{req.tool_name}</code>
          </div>
          {req.action && (
            <div>
              <strong style={{ color: 'var(--fg)' }}>Acción:</strong> {req.action}
            </div>
          )}
          {req.path && (
            <div style={{ wordBreak: 'break-all' }}>
              <strong style={{ color: 'var(--fg)' }}>Ruta:</strong>{' '}
              <code style={{ fontSize: 12 }}>{req.path}</code>
            </div>
          )}
          {req.description && (
            <div style={{ marginTop: '0.5rem', whiteSpace: 'pre-wrap' }}>{req.description}</div>
          )}
        </div>

        {pending.length > 1 && (
          <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginBottom: '0.75rem' }}>
            +{pending.length - 1} solicitud(es) en cola
          </div>
        )}

        <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', flexWrap: 'wrap' }}>
          <button
            onClick={() => act('deny')}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg)',
              fontSize: 13,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Denegar
          </button>
          <button
            onClick={() => act('allow_session')}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg)',
              fontSize: 13,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Permitir en la sesión
          </button>
          <button
            onClick={() => act('allow')}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: 'var(--primary)',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Permitir
          </button>
        </div>
      </div>
    </div>
  )
}
