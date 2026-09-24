import StatusBadge from '@/components/shared/StatusBadge'
import ProgressBar from '@/components/shared/ProgressBar'
import { IconButton } from '@/components/ui'
import { CircleStop, Trash2 } from '@/components/ui/icons'
import type { OrchestratorTask } from '@pando/client/types'
import { useOrchestratorStore } from '@pando/client/stores/orchestratorStore'

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

  return (
    <tr onClick={onClick} data-clickable="true" data-selected={selected || undefined}>
      <td>
        <StatusBadge status={task.status} />
      </td>
      <td className="is-ellipsis font-medium" style={{ maxWidth: 200 }}>
        {task.name}
      </td>
      <td className="is-muted">{task.agent}</td>
      <td className="is-mono">{task.model}</td>
      <td style={{ minWidth: 120 }}>
        <ProgressBar value={task.progress} />
      </td>
      <td className="is-numeric is-mono">{task.tokens.toLocaleString()}</td>
      <td onClick={(e) => e.stopPropagation()}>
        <div className="flex gap-1.5">
          {task.status === 'running' && (
            <IconButton
              aria-label="Cancel task"
              tooltip
              icon={<CircleStop size={14} />}
              size="sm"
              variant="danger"
              onClick={() => cancelTask(task.id)}
            />
          )}
          {task.status !== 'running' && (
            <IconButton
              aria-label="Delete task"
              tooltip
              icon={<Trash2 size={14} />}
              size="sm"
              onClick={() => deleteTask(task.id)}
            />
          )}
        </div>
      </td>
    </tr>
  )
}
