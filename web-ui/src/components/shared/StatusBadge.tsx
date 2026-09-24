import { Badge, type BadgeTone } from '@/components/ui'

type Status = 'running' | 'completed' | 'error' | 'pending' | 'active' | 'archived' | 'cancelled'

const STATUS_CONFIG: Record<Status, { label: string; tone: BadgeTone; dot?: boolean }> = {
  running: { label: 'Running', tone: 'success', dot: true },
  completed: { label: 'Completed', tone: 'neutral' },
  error: { label: 'Error', tone: 'danger' },
  pending: { label: 'Pending', tone: 'warning' },
  active: { label: 'Active', tone: 'success', dot: true },
  archived: { label: 'Archived', tone: 'neutral' },
  cancelled: { label: 'Cancelled', tone: 'neutral' },
}

export default function StatusBadge({ status }: { status: Status }) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.pending
  return (
    <Badge tone={cfg.tone} dot={cfg.dot}>
      {cfg.label}
    </Badge>
  )
}
