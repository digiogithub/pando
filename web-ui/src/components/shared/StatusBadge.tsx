type Status = 'running' | 'completed' | 'error' | 'pending' | 'active' | 'archived' | 'cancelled'

const STATUS_CONFIG: Record<Status, { label: string; bg: string; color: string }> = {
  running:   { label: 'Running',   bg: 'var(--success)',   color: 'white' },
  completed: { label: 'Completed', bg: 'var(--secondary)', color: 'var(--fg)' },
  error:     { label: 'Error',     bg: 'var(--error)',     color: 'white' },
  pending:   { label: 'Pending',   bg: 'var(--warning)',   color: 'var(--fg)' },
  active:    { label: 'Active',    bg: 'var(--success)',   color: 'white' },
  archived:  { label: 'Archived',  bg: 'var(--secondary)', color: 'var(--fg)' },
  cancelled: { label: 'Cancelled', bg: 'var(--fg-dim)',    color: 'white' },
}

export default function StatusBadge({ status }: { status: Status }) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.pending
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        padding: '0.125rem 0.5rem',
        borderRadius: 0,
        fontSize: 11,
        fontWeight: 600,
        background: cfg.bg,
        color: cfg.color,
      }}
    >
      {cfg.label}
    </span>
  )
}
