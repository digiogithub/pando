import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faSearch, faArrowDown, faSyncAlt } from '@fortawesome/free-solid-svg-icons'
import { useLogsStore } from '@/stores/logsStore'
import type { LogLevel } from '@/stores/logsStore'

const LEVELS: { value: LogLevel; label: string; color: string }[] = [
  { value: 'all', label: 'All', color: 'var(--fg)' },
  { value: 'debug', label: 'DEBUG', color: 'var(--fg-dim)' },
  { value: 'info', label: 'INFO', color: 'var(--info)' },
  { value: 'warn', label: 'WARN', color: 'var(--warning)' },
  { value: 'error', label: 'ERROR', color: 'var(--error)' },
]

export default function LogFilters() {
  const { levelFilter, searchQuery, autoScroll, setLevelFilter, setSearchQuery, setAutoScroll, fetchLogs } =
    useLogsStore()

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.75rem',
        padding: '0.625rem 1rem',
        borderBottom: '1px solid var(--border)',
        background: 'var(--card-bg)',
        flexShrink: 0,
        flexWrap: 'wrap',
      }}
    >
      {/* Level dropdown */}
      <select
        value={levelFilter}
        onChange={(e) => setLevelFilter(e.target.value as LogLevel)}
        style={{
          background: 'var(--input-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          color: LEVELS.find((l) => l.value === levelFilter)?.color ?? 'var(--fg)',
          fontSize: 13,
          fontWeight: 600,
          padding: '0.375rem 0.625rem',
          cursor: 'pointer',
          outline: 'none',
          fontFamily: 'inherit',
        }}
      >
        {LEVELS.map((l) => (
          <option key={l.value} value={l.value} style={{ color: l.color }}>
            {l.label}
          </option>
        ))}
      </select>

      {/* Search input */}
      <div style={{ position: 'relative', flex: 1, minWidth: 160 }}>
        <FontAwesomeIcon
          icon={faSearch}
          style={{
            position: 'absolute',
            left: 10,
            top: '50%',
            transform: 'translateY(-50%)',
            color: 'var(--fg-dim)',
            fontSize: 12,
            pointerEvents: 'none',
          }}
        />
        <input
          type="text"
          placeholder="Search logs…"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          style={{
            background: 'var(--input-bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--fg)',
            fontSize: 13,
            padding: '0.375rem 0.625rem 0.375rem 2rem',
            outline: 'none',
            width: '100%',
            fontFamily: 'inherit',
            boxSizing: 'border-box',
          }}
          onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
          onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
        />
      </div>

      {/* Auto-scroll toggle */}
      <button
        title={autoScroll ? 'Auto-scroll on' : 'Auto-scroll off'}
        onClick={() => setAutoScroll(!autoScroll)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.375rem',
          padding: '0.375rem 0.625rem',
          background: autoScroll ? 'var(--selected)' : 'transparent',
          border: `1px solid ${autoScroll ? 'var(--primary)' : 'var(--border)'}`,
          borderRadius: 'var(--radius-sm)',
          color: autoScroll ? 'var(--primary)' : 'var(--fg-muted)',
          fontSize: 12,
          fontWeight: 600,
          cursor: 'pointer',
          fontFamily: 'inherit',
          transition: 'all 0.15s',
        }}
      >
        <FontAwesomeIcon icon={faArrowDown} />
        Auto-scroll
      </button>

      {/* Refresh button */}
      <button
        title="Refresh logs"
        onClick={fetchLogs}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 32,
          height: 32,
          background: 'transparent',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--fg-muted)',
          cursor: 'pointer',
          transition: 'color 0.15s, border-color 0.15s',
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.color = 'var(--fg)'
          e.currentTarget.style.borderColor = 'var(--border-focus)'
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.color = 'var(--fg-muted)'
          e.currentTarget.style.borderColor = 'var(--border)'
        }}
      >
        <FontAwesomeIcon icon={faSyncAlt} style={{ fontSize: 13 }} />
      </button>
    </div>
  )
}
