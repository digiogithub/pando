import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTimes, faRobot, faMicrochip, faClock } from '@fortawesome/free-solid-svg-icons'
import type { OrchestratorTask, OrchestratorToolCall } from '@/types'
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
    <div
      style={{
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-sm)',
        padding: '0.75rem',
        background: 'var(--bg)',
        display: 'flex',
        flexDirection: 'column',
        gap: '0.5rem',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'center' }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--fg)' }}>{toolCall.title || toolCall.name}</div>
          <div style={{ fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'monospace' }}>{toolCall.name}</div>
        </div>
        <StatusBadge status={toolCall.status as 'running' | 'completed' | 'error' | 'pending'} />
      </div>

      {toolCall.arguments && Object.keys(toolCall.arguments).length > 0 && (
        <div>
          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', marginBottom: '0.25rem', textTransform: 'uppercase' }}>
            Input
          </div>
          <pre style={preStyle}>{formatStructured(toolCall.arguments)}</pre>
        </div>
      )}

      {toolCall.result && (
        <div>
          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', marginBottom: '0.25rem', textTransform: 'uppercase' }}>
            Result
          </div>
          <pre style={preStyle}>{toolCall.result}</pre>
        </div>
      )}

      {toolCall.locations && toolCall.locations.length > 0 && (
        <div>
          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', marginBottom: '0.25rem', textTransform: 'uppercase' }}>
            Locations
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            {toolCall.locations.map((location) => (
              <code key={location} style={{ fontSize: 11, color: 'var(--fg)', wordBreak: 'break-all' }}>{location}</code>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

const preStyle: React.CSSProperties = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  padding: '0.625rem 0.75rem',
  fontSize: 12,
  fontFamily: 'monospace',
  color: 'var(--fg)',
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
  overflow: 'auto',
  margin: 0,
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

      <div style={{ flex: 1, overflow: 'auto', padding: '1rem' }}>
        <div style={{ marginBottom: '1rem' }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--fg)', marginBottom: '0.375rem' }}>
            {task.name}
          </div>
          <StatusBadge status={task.status} />
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '1rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <FontAwesomeIcon icon={faRobot} style={{ width: 12 }} />
            <span>Agent: <span style={{ color: 'var(--fg)', fontWeight: 500 }}>{task.agent}</span></span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <FontAwesomeIcon icon={faMicrochip} style={{ width: 12 }} />
            <span>Model: <span style={{ color: 'var(--fg)', fontWeight: 500, fontFamily: 'monospace' }}>{task.model}</span></span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <FontAwesomeIcon icon={faClock} style={{ width: 12 }} />
            <span>Created: <span style={{ color: 'var(--fg)' }}>{formatDate(task.created_at)}</span></span>
          </div>
          {task.current_tool && (
            <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
              Current tool: <span style={{ color: 'var(--fg)', fontWeight: 500 }}>{task.current_tool}</span>
            </div>
          )}
        </div>

        <div style={{ marginBottom: '1rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: 'var(--fg-muted)', marginBottom: '0.375rem' }}>
            <span>Progress</span>
            <span>{task.progress}%</span>
          </div>
          <ProgressBar value={task.progress} />
        </div>

        <div style={{ marginBottom: '1rem' }}>
          <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginBottom: '0.25rem' }}>Tokens used</div>
          <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--primary)', fontFamily: 'monospace' }}>
            {task.tokens.toLocaleString()}
          </div>
        </div>

        {task.tool_calls && task.tool_calls.length > 0 && (
          <div style={{ marginBottom: '1rem' }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', marginBottom: '0.375rem', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              Tool Calls
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              {task.tool_calls.map((toolCall) => (
                <ToolCallCard key={toolCall.id} toolCall={toolCall} />
              ))}
            </div>
          </div>
        )}

        {task.output && (
          <div>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', marginBottom: '0.375rem', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              Output
            </div>
            <pre style={{ ...preStyle, maxHeight: 300 }}>{task.output}</pre>
          </div>
        )}
      </div>
    </div>
  )
}
