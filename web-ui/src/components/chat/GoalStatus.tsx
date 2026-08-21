import type { CSSProperties } from 'react'
import type { GoalStatus as GoalStatusModel } from '@pando/client/types'

interface GoalStatusProps {
  goal: GoalStatusModel | null
  loading?: boolean
  cancelling?: boolean
  onCancel?: () => void
}

const badgeColors: Record<string, string> = {
  running: 'var(--primary)',
  completed: 'var(--success)',
  blocked: 'var(--warning)',
  cancelled: 'var(--error)',
}

const panelStyle: CSSProperties = {
  margin: '0.75rem 1rem 0.5rem',
  padding: '0.75rem 0.875rem',
  border: '1px solid var(--border)',
  background: 'var(--bg-secondary)',
  display: 'flex',
  flexDirection: 'column',
  gap: '0.5rem',
}

function formatTimestamp(value?: number): string | null {
  if (!value) return null
  return new Date(value * 1000).toLocaleString()
}

export default function GoalStatus({ goal, loading = false, cancelling = false, onCancel }: GoalStatusProps) {
  if (!goal && !loading) return null

  const statusColor = badgeColors[goal?.status ?? ''] ?? 'var(--fg-dim)'
  const completedAt = formatTimestamp(goal?.completedAt)

  return (
    <div style={panelStyle}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          <span
            style={{
              fontSize: 11,
              letterSpacing: '0.08em',
              textTransform: 'uppercase',
              color: 'var(--fg-dim)',
            }}
          >
            Goal mode
          </span>
          <span
            style={{
              padding: '0.15rem 0.45rem',
              border: `1px solid color-mix(in srgb, ${statusColor} 40%, transparent)`,
              color: statusColor,
              fontSize: 11,
              textTransform: 'uppercase',
            }}
          >
            {loading && !goal ? 'loading' : goal?.status ?? 'idle'}
          </span>
        </div>
        {goal?.status === 'running' && onCancel && (
          <button
            type="button"
            onClick={onCancel}
            disabled={cancelling}
            style={{
              border: '1px solid var(--error)',
              background: cancelling ? 'color-mix(in srgb, var(--error) 15%, transparent)' : 'transparent',
              color: 'var(--error)',
              padding: '0.3rem 0.6rem',
              cursor: cancelling ? 'wait' : 'pointer',
              fontSize: 12,
            }}
          >
            {cancelling ? 'Cancelling…' : 'Cancel'}
          </button>
        )}
      </div>

      {goal ? (
        <>
          <div style={{ fontSize: 14, color: 'var(--fg)', fontWeight: 600 }}>{goal.objective}</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            <span>Iteration {goal.iteration}/{goal.maxIterations}</span>
            {goal.startedAt > 0 && <span>Started {formatTimestamp(goal.startedAt)}</span>}
            {completedAt && <span>Finished {completedAt}</span>}
          </div>
          {goal.progress && (
            <div style={{ fontSize: 13, color: 'var(--fg)' }}>
              <strong>Progress:</strong> {goal.progress}
            </div>
          )}
          {goal.nextStep && goal.status === 'running' && (
            <div style={{ fontSize: 13, color: 'var(--fg)' }}>
              <strong>Next step:</strong> {goal.nextStep}
            </div>
          )}
        </>
      ) : (
        <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>Loading goal status…</div>
      )}
    </div>
  )
}
