import { useEffect, useRef } from 'react'
import { Badge, type BadgeTone } from '@/components/ui'
import { useLogsStore } from '@pando/client/stores/logsStore'
import type { LogEntry } from '@pando/client/types'

const LEVEL_TONE: Record<string, BadgeTone> = {
  debug: 'neutral',
  info: 'info',
  warn: 'warning',
  error: 'danger',
}

function LevelBadge({ level }: { level: string }) {
  return (
    <Badge tone={LEVEL_TONE[level] ?? 'neutral'} outline className="uppercase tracking-wide">
      {level}
    </Badge>
  )
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts)
    return d.toLocaleTimeString('en-GB', { hour12: false })
  } catch {
    return ts
  }
}

export default function LogTable() {
  const { entries, selectedEntry, levelFilter, searchQuery, autoScroll, setSelectedEntry } =
    useLogsStore()
  const bottomRef = useRef<HTMLDivElement>(null)

  // Filter entries
  const filtered = entries.filter((e) => {
    if (levelFilter !== 'all' && e.level !== levelFilter) return false
    if (searchQuery) {
      const q = searchQuery.toLowerCase()
      return (
        e.message.toLowerCase().includes(q) ||
        e.source.toLowerCase().includes(q) ||
        e.level.toLowerCase().includes(q)
      )
    }
    return true
  })

  // Auto-scroll when new entries arrive
  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [filtered.length, autoScroll])

  if (filtered.length === 0) {
    return (
      <div className="centered-fill">
        <span className="text-sm">No log entries found.</span>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <table className="view-table log-table" style={{ tableLayout: 'fixed' }}>
        <colgroup>
          <col style={{ width: 90 }} />
          <col style={{ width: 80 }} />
          <col style={{ width: 130 }} />
          <col />
        </colgroup>
        <thead>
          <tr>
            <th>Time</th>
            <th>Level</th>
            <th>Source</th>
            <th>Message</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map((entry: LogEntry) => {
            const isSelected = selectedEntry?.id === entry.id
            return (
              <tr
                key={entry.id}
                onClick={() => setSelectedEntry(isSelected ? null : entry)}
                data-clickable="true"
                data-selected={isSelected || undefined}
              >
                <td className="log-row-time">{formatTime(entry.timestamp)}</td>
                <td>
                  <LevelBadge level={entry.level} />
                </td>
                <td className="is-ellipsis log-row-source">{entry.source}</td>
                <td className="is-ellipsis log-row-message">{entry.message}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <div ref={bottomRef} />
    </div>
  )
}
