import { Bot, Cpu, Clock, User, X } from '@/components/ui/icons'
import { IconButton } from '@/components/ui'
import type { OrchestratorTask, OrchestratorToolCall } from '@pando/client/types'
import StatusBadge from '@/components/shared/StatusBadge'
import ProgressBar from '@/components/shared/ProgressBar'

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function formatStructured(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function ToolCallCard({ toolCall }: { toolCall: OrchestratorToolCall }) {
  return (
    <div className="entity-card">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm font-semibold text-fg">{toolCall.title || toolCall.name}</div>
          <div className="font-mono text-xs text-muted">{toolCall.name}</div>
        </div>
        <StatusBadge status={toolCall.status as 'running' | 'completed' | 'error' | 'pending'} />
      </div>

      {toolCall.arguments && Object.keys(toolCall.arguments).length > 0 && (
        <div>
          <div className="detail-field-label">Input</div>
          <pre className="code-block">{formatStructured(toolCall.arguments)}</pre>
        </div>
      )}

      {toolCall.result && (
        <div>
          <div className="detail-field-label">Result</div>
          <pre className="code-block">{toolCall.result}</pre>
        </div>
      )}

      {toolCall.locations && toolCall.locations.length > 0 && (
        <div>
          <div className="detail-field-label">Locations</div>
          <div className="flex flex-col gap-1">
            {toolCall.locations.map((location) => (
              <code key={location} className="break-all text-xs text-fg">{location}</code>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export default function TaskDetail({
  task,
  onClose,
}: {
  task: OrchestratorTask
  onClose: () => void
}) {
  return (
    <div className="view-detail">
      <div className="view-detail-header">
        <span className="view-detail-title">Task Detail</span>
        <IconButton aria-label="Close detail" tooltip icon={<X size={14} />} size="sm" onClick={onClose} />
      </div>

      <div className="view-detail-body">
        <div>
          <div className="mb-1.5 text-[15px] font-semibold text-fg">{task.name}</div>
          <StatusBadge status={task.status} />
        </div>

        {task.prompt && (
          <div>
            <div className="detail-field-label">Prompt</div>
            <div className="whitespace-pre-wrap break-words rounded-md border-l-2 border-accent bg-accent-soft px-3 py-2.5 text-xs text-fg">
              {task.prompt}
            </div>
          </div>
        )}

        <div className="flex flex-col gap-2">
          <div className="detail-meta-row">
            <Bot size={12} />
            <span>Agent: <span className="value">{task.agent}</span></span>
          </div>
          <div className="detail-meta-row">
            <Cpu size={12} />
            <span>Model: <span className="value font-mono">{task.model}</span></span>
          </div>
          {task.persona && (
            <div className="detail-meta-row">
              <User size={12} />
              <span>Persona: <span className="value">{task.persona}</span></span>
            </div>
          )}
          <div className="detail-meta-row">
            <Clock size={12} />
            <span>Created: <span className="value font-normal">{formatDate(task.created_at)}</span></span>
          </div>
          {task.current_tool && (
            <div className="text-sm text-muted">
              Current tool: <span className="value">{task.current_tool}</span>
            </div>
          )}
        </div>

        <div>
          <div className="mb-1.5 flex justify-between text-xs text-muted">
            <span>Progress</span>
            <span>{task.progress}%</span>
          </div>
          <ProgressBar value={task.progress} />
        </div>

        <div>
          <div className="mb-1 text-xs text-muted">Tokens used</div>
          <div className="font-mono text-lg font-semibold text-fg">{task.tokens.toLocaleString()}</div>
        </div>

        {task.tool_calls && task.tool_calls.length > 0 && (
          <div>
            <div className="detail-field-label">Tool Calls</div>
            <div className="flex flex-col gap-3">
              {task.tool_calls.map((toolCall) => (
                <ToolCallCard key={toolCall.id} toolCall={toolCall} />
              ))}
            </div>
          </div>
        )}

        {task.output && (
          <div>
            <div className="detail-field-label">Output</div>
            <pre className="code-block" style={{ maxHeight: 300 }}>{task.output}</pre>
          </div>
        )}
      </div>
    </div>
  )
}
