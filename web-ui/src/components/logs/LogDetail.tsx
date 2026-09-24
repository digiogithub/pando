import { Badge, IconButton, type BadgeTone } from '@/components/ui'
import { X } from '@/components/ui/icons'
import { useLogsStore } from '@pando/client/stores/logsStore'

const LEVEL_TONE: Record<string, BadgeTone> = {
  debug: 'neutral',
  info: 'info',
  warn: 'warning',
  error: 'danger',
}

function formatFullTimestamp(ts: string): string {
  try {
    return new Date(ts).toLocaleString('en-GB', { hour12: false })
  } catch {
    return ts
  }
}

export default function LogDetail() {
  const { selectedEntry, setSelectedEntry } = useLogsStore()

  if (!selectedEntry) return null

  return (
    <div className="view-detail view-detail--bottom">
      {/* Header */}
      <div className="view-detail-header">
        <span className="view-detail-title">Log Detail</span>
        <IconButton aria-label="Close detail" tooltip icon={<X size={14} />} size="sm" onClick={() => setSelectedEntry(null)} />
      </div>

      {/* Content */}
      <div className="flex flex-1 flex-col gap-2 overflow-y-auto px-4 py-3">
        {/* Meta row */}
        <div className="flex flex-wrap items-center gap-4">
          <span className="font-mono text-xs text-faint">{formatFullTimestamp(selectedEntry.timestamp)}</span>

          <Badge tone={LEVEL_TONE[selectedEntry.level] ?? 'neutral'} outline className="uppercase tracking-wide">
            {selectedEntry.level}
          </Badge>

          <span className="text-xs text-muted">
            Source: <strong className="font-semibold text-fg">{selectedEntry.source}</strong>
          </span>
        </div>

        {/* Message */}
        <p className="break-words text-sm text-fg">{selectedEntry.message}</p>

        {/* Details / stack trace */}
        {selectedEntry.details && <pre className="code-block">{selectedEntry.details}</pre>}
      </div>
    </div>
  )
}
