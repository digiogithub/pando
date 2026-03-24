import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTimes } from '@fortawesome/free-solid-svg-icons'
import { useLogsStore } from '@/stores/logsStore'

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
    <div
      style={{
        height: 200,
        flexShrink: 0,
        borderTop: '1px solid var(--border)',
        background: 'var(--card-bg)',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.5rem 1rem',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
        }}
      >
        <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
          Log Detail
        </span>
        <button
          onClick={() => setSelectedEntry(null)}
          style={{
            background: 'transparent',
            border: 'none',
            color: 'var(--fg-muted)',
            cursor: 'pointer',
            padding: '0.25rem',
            display: 'flex',
            alignItems: 'center',
            fontSize: 13,
          }}
          title="Close detail"
        >
          <FontAwesomeIcon icon={faTimes} />
        </button>
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflowY: 'auto', padding: '0.75rem 1rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        {/* Meta row */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <span style={{ fontSize: 12, color: 'var(--fg-dim)', fontFamily: 'monospace' }}>
            {formatFullTimestamp(selectedEntry.timestamp)}
          </span>

          <span
            style={{
              display: 'inline-block',
              padding: '0.1rem 0.5rem',
              borderRadius: 6,
              fontSize: 11,
              fontWeight: 700,
              letterSpacing: '0.04em',
              textTransform: 'uppercase',
              color: LEVEL_COLORS[selectedEntry.level] ?? 'var(--fg)',
              background: LEVEL_BG[selectedEntry.level] ?? 'transparent',
              border: `1px solid ${LEVEL_COLORS[selectedEntry.level] ?? 'var(--border)'}`,
            }}
          >
            {selectedEntry.level}
          </span>

          <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
            Source: <strong style={{ color: 'var(--fg)' }}>{selectedEntry.source}</strong>
          </span>
        </div>

        {/* Message */}
        <p style={{ fontSize: 13, color: 'var(--fg)', margin: 0, wordBreak: 'break-word' }}>
          {selectedEntry.message}
        </p>

        {/* Details / stack trace */}
        {selectedEntry.details && (
          <pre
            style={{
              background: 'var(--surface)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              padding: '0.625rem 0.75rem',
              fontSize: 12,
              color: 'var(--fg-muted)',
              fontFamily: 'monospace',
              overflowX: 'auto',
              margin: 0,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
            }}
          >
            {selectedEntry.details}
          </pre>
        )}
      </div>
    </div>
  )
}
