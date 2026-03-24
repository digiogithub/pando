import { useEffect, useRef } from 'react'
import { useLogsStore } from '@/stores/logsStore'
import type { LogEntry } from '@/types'

const LEVEL_COLORS: Record<string, string> = {
  debug: 'var(--fg-dim)',
  info: 'var(--info)',
  warn: 'var(--warning)',
  error: 'var(--error)',
}

const LEVEL_BG: Record<string, string> = {
  debug: 'transparent',
  info: 'rgba(41,128,185,0.12)',
  warn: 'rgba(232,201,75,0.15)',
  error: 'rgba(192,57,43,0.12)',
}

function LevelBadge({ level }: { level: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '0.125rem 0.5rem',
        borderRadius: 6,
        fontSize: 11,
        fontWeight: 700,
        letterSpacing: '0.04em',
        textTransform: 'uppercase',
        color: LEVEL_COLORS[level] ?? 'var(--fg)',
        background: LEVEL_BG[level] ?? 'transparent',
        border: `1px solid ${LEVEL_COLORS[level] ?? 'var(--border)'}`,
        whiteSpace: 'nowrap',
      }}
    >
      {level}
    </span>
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
      <div
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'var(--fg-muted)',
          fontSize: 14,
          padding: '2rem',
        }}
      >
        No log entries found.
      </div>
    )
  }

  const colStyle = (width?: string | number): React.CSSProperties => ({
    padding: '0.5rem 0.75rem',
    fontSize: 13,
    color: 'var(--fg-muted)',
    fontWeight: 600,
    borderBottom: '1px solid var(--border)',
    whiteSpace: 'nowrap',
    width: width ?? 'auto',
  })

  const cellStyle: React.CSSProperties = {
    padding: '0.4rem 0.75rem',
    fontSize: 13,
    color: 'var(--fg)',
    borderBottom: '1px solid var(--border)',
    verticalAlign: 'middle',
  }

  return (
    <div style={{ flex: 1, overflow: 'auto' }}>
      <table style={{ width: '100%', borderCollapse: 'collapse', tableLayout: 'fixed' }}>
        <colgroup>
          <col style={{ width: 90 }} />
          <col style={{ width: 80 }} />
          <col style={{ width: 110 }} />
          <col />
        </colgroup>
        <thead style={{ position: 'sticky', top: 0, background: 'var(--card-bg)', zIndex: 1 }}>
          <tr>
            <th style={{ ...colStyle(90), textAlign: 'left' }}>Time</th>
            <th style={{ ...colStyle(80), textAlign: 'left' }}>Level</th>
            <th style={{ ...colStyle(110), textAlign: 'left' }}>Source</th>
            <th style={{ ...colStyle(), textAlign: 'left' }}>Message</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map((entry: LogEntry) => {
            const isSelected = selectedEntry?.id === entry.id
            return (
              <tr
                key={entry.id}
                onClick={() => setSelectedEntry(isSelected ? null : entry)}
                style={{
                  background: isSelected ? 'var(--selected)' : 'transparent',
                  cursor: 'pointer',
                  transition: 'background 0.1s',
                }}
                onMouseEnter={(e) => {
                  if (!isSelected) e.currentTarget.style.background = 'var(--hover)'
                }}
                onMouseLeave={(e) => {
                  if (!isSelected) e.currentTarget.style.background = 'transparent'
                }}
              >
                <td style={{ ...cellStyle, color: 'var(--fg-dim)', fontFamily: 'monospace', fontSize: 12 }}>
                  {formatTime(entry.timestamp)}
                </td>
                <td style={cellStyle}>
                  <LevelBadge level={entry.level} />
                </td>
                <td
                  style={{
                    ...cellStyle,
                    color: 'var(--fg-muted)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {entry.source}
                </td>
                <td
                  style={{
                    ...cellStyle,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {entry.message}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <div ref={bottomRef} />
    </div>
  )
}
