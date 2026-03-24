import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTimes, faRobot, faMicrochip, faClock } from '@fortawesome/free-solid-svg-icons'
import type { OrchestratorTask } from '@/types'
import StatusBadge from '@/components/shared/StatusBadge'
import ProgressBar from '@/components/shared/ProgressBar'

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export default function TaskDetail({
  task,
  onClose,
}: {
  task: OrchestratorTask
  onClose: () => void
}) {
  return (
    <div
      style={{
        width: 300,
        flexShrink: 0,
        borderLeft: '1px solid var(--border)',
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--surface)',
        overflow: 'hidden',
      }}
    >
      {/* Detail header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.75rem 1rem',
          borderBottom: '1px solid var(--border)',
        }}
      >
        <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--fg)' }}>Task Detail</span>
        <button
          onClick={onClose}
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            padding: '0.2rem',
          }}
          title="Close detail"
        >
          <FontAwesomeIcon icon={faTimes} />
        </button>
      </div>

      {/* Detail body */}
      <div style={{ flex: 1, overflow: 'auto', padding: '1rem' }}>
        {/* Name + status */}
        <div style={{ marginBottom: '1rem' }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--fg)', marginBottom: '0.375rem' }}>
            {task.name}
          </div>
          <StatusBadge status={task.status} />
        </div>

        {/* Meta rows */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '1rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <FontAwesomeIcon icon={faRobot} style={{ width: 12 }} />
            <span>Agent: <span style={{ color: 'var(--fg)', fontWeight: 500 }}>{task.agent}</span></span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <FontAwesomeIcon icon={faCpuBolt} style={{ width: 12 }} />
            <span>Model: <span style={{ color: 'var(--fg)', fontWeight: 500, fontFamily: 'monospace' }}>{task.model}</span></span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <FontAwesomeIcon icon={faClock} style={{ width: 12 }} />
            <span>Created: <span style={{ color: 'var(--fg)' }}>{formatDate(task.created_at)}</span></span>
          </div>
        </div>

        {/* Progress */}
        <div style={{ marginBottom: '1rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: 'var(--fg-muted)', marginBottom: '0.375rem' }}>
            <span>Progress</span>
            <span>{task.progress}%</span>
          </div>
          <ProgressBar value={task.progress} />
        </div>

        {/* Tokens */}
        <div style={{ marginBottom: '1rem' }}>
          <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginBottom: '0.25rem' }}>Tokens used</div>
          <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--primary)', fontFamily: 'monospace' }}>
            {task.tokens.toLocaleString()}
          </div>
        </div>

        {/* Output */}
        {task.output && (
          <div>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', marginBottom: '0.375rem', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              Output
            </div>
            <pre
              style={{
                background: 'var(--bg)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                padding: '0.625rem 0.75rem',
                fontSize: 12,
                fontFamily: 'monospace',
                color: 'var(--fg)',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                maxHeight: 300,
                overflow: 'auto',
                margin: 0,
              }}
            >
              {task.output}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}
