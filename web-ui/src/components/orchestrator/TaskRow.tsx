import StatusBadge from '@/components/shared/StatusBadge'
import ProgressBar from '@/components/shared/ProgressBar'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faStop, faTrash } from '@fortawesome/free-solid-svg-icons'
import type { OrchestratorTask } from '@/types'
import { useOrchestratorStore } from '@/stores/orchestratorStore'

export default function TaskRow({
  task,
  selected,
  onClick,
}: {
  task: OrchestratorTask
  selected: boolean
  onClick: () => void
}) {
  const { cancelTask, deleteTask } = useOrchestratorStore()

  const cellStyle: React.CSSProperties = {
    padding: '0.5rem 0.75rem',
    fontSize: 13,
    color: 'var(--fg)',
    verticalAlign: 'middle',
    whiteSpace: 'nowrap',
  }

  return (
    <tr
      onClick={onClick}
      style={{
        cursor: 'pointer',
        borderBottom: '1px solid var(--border)',
        background: selected ? 'var(--surface-active, var(--hover))' : 'transparent',
      }}
      onMouseEnter={(e) => {
        if (!selected) e.currentTarget.style.background = 'var(--hover)'
      }}
      onMouseLeave={(e) => {
        if (!selected) e.currentTarget.style.background = 'transparent'
      }}
    >
      <td style={cellStyle}>
        <StatusBadge status={task.status} />
      </td>
      <td
        style={{
          ...cellStyle,
          maxWidth: 200,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          fontWeight: 500,
        }}
      >
        {task.name}
      </td>
      <td style={{ ...cellStyle, color: 'var(--fg-muted)' }}>{task.agent}</td>
      <td
        style={{
          ...cellStyle,
          color: 'var(--fg-muted)',
          fontFamily: 'monospace',
          fontSize: 11,
        }}
      >
        {task.model}
      </td>
      <td style={{ ...cellStyle, minWidth: 120 }}>
        <ProgressBar value={task.progress} />
      </td>
      <td
        style={{
          ...cellStyle,
          color: 'var(--fg-muted)',
          fontFamily: 'monospace',
          textAlign: 'right',
        }}
      >
        {task.tokens.toLocaleString()}
      </td>
      <td style={cellStyle} onClick={(e) => e.stopPropagation()}>
        <div style={{ display: 'flex', gap: '0.375rem' }}>
          {task.status === 'running' && (
            <button
              onClick={() => cancelTask(task.id)}
              title="Cancel task"
              style={{
                background: 'none',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                padding: '0.2rem 0.5rem',
                cursor: 'pointer',
                color: 'var(--error)',
              }}
            >
              <FontAwesomeIcon icon={faStop} style={{ fontSize: 11 }} />
            </button>
          )}
          {task.status !== 'running' && (
            <button
              onClick={() => deleteTask(task.id)}
              title="Delete task"
              style={{
                background: 'none',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                padding: '0.2rem 0.5rem',
                cursor: 'pointer',
                color: 'var(--fg-muted)',
              }}
            >
              <FontAwesomeIcon icon={faTrash} style={{ fontSize: 11 }} />
            </button>
          )}
        </div>
      </td>
    </tr>
  )
}
