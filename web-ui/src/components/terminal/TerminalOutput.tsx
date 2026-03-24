import { useEffect, useRef } from 'react'
import type { TerminalEntry } from '@/stores/terminalStore'

export default function TerminalOutput({ entries }: { entries: TerminalEntry[] }) {
  const bottomRef = useRef<HTMLDivElement>(null)

  // Auto-scroll to bottom when entries change
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [entries])

  return (
    <div
      style={{
        flex: 1,
        overflow: 'auto',
        background: '#0d1117',
        padding: '0.75rem 1rem',
        fontFamily: 'monospace',
        fontSize: 13,
        lineHeight: 1.6,
      }}
    >
      {entries.length === 0 && (
        <div style={{ color: '#6e7681', fontStyle: 'italic' }}>
          Pando Terminal — type a command to get started
        </div>
      )}
      {entries.map((entry) => (
        <div key={entry.id}>
          {entry.type === 'command' ? (
            <div style={{ color: '#58a6ff' }}>
              <span style={{ color: '#3fb950' }}>$ </span>
              <span style={{ whiteSpace: 'pre-wrap' }}>{entry.text}</span>
            </div>
          ) : entry.type === 'error' ? (
            <div style={{ color: 'var(--error)', whiteSpace: 'pre-wrap' }}>{entry.text}</div>
          ) : (
            <div style={{ color: '#e2e8f0', whiteSpace: 'pre-wrap' }}>{entry.text}</div>
          )}
        </div>
      ))}
      <div ref={bottomRef} />
    </div>
  )
}
