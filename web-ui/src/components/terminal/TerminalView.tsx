import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTerminal, faTrash } from '@fortawesome/free-solid-svg-icons'
import { useTerminalStore } from '@/stores/terminalStore'
import TerminalOutput from './TerminalOutput'
import TerminalInput from './TerminalInput'
import LoadingSpinner from '@/components/shared/LoadingSpinner'

export default function TerminalView() {
  const { entries, running, clearEntries } = useTerminalStore()

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
        background: '#0d1117',
      }}
    >
      {/* Toolbar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.625rem 1rem',
          background: '#161b22',
          borderBottom: '1px solid #30363d',
          flexShrink: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <FontAwesomeIcon icon={faTerminal} style={{ color: '#3fb950', fontSize: 13 }} />
          <span style={{ fontSize: 13, fontWeight: 600, color: '#e2e8f0' }}>Terminal</span>
          {running && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.375rem' }}>
              <LoadingSpinner size={12} />
              <span style={{ fontSize: 11, color: '#6e7681' }}>running…</span>
            </div>
          )}
        </div>

        <button
          onClick={clearEntries}
          title="Clear terminal"
          disabled={entries.length === 0}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.3rem',
            background: 'none',
            border: '1px solid #30363d',
            borderRadius: 'var(--radius-sm)',
            padding: '0.25rem 0.6rem',
            cursor: entries.length === 0 ? 'not-allowed' : 'pointer',
            color: entries.length === 0 ? '#6e7681' : '#8b949e',
            fontSize: 12,
          }}
        >
          <FontAwesomeIcon icon={faTrash} style={{ fontSize: 10 }} />
          Clear
        </button>
      </div>

      {/* Output area */}
      <TerminalOutput entries={entries} />

      {/* Input bar */}
      <TerminalInput />
    </div>
  )
}
