import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCamera } from '@fortawesome/free-solid-svg-icons'
import type { Snapshot } from '@/types'
import EmptyState from '@/components/shared/EmptyState'
import SnapshotRow from './SnapshotRow'

const HEADERS = ['Name', 'Session', 'Status', 'Date', 'Size', 'Actions']
const WIDTHS = ['30%', '10%', '12%', '13%', '10%', '15%']

interface SnapshotTableProps {
  snapshots: Snapshot[]
  onCreateClick: () => void
}

export default function SnapshotTable({ snapshots, onCreateClick }: SnapshotTableProps) {
  if (snapshots.length === 0) {
    return (
      <EmptyState
        icon={<FontAwesomeIcon icon={faCamera} />}
        title="No snapshots yet"
        description="Snapshots let you save and restore session states at any point in time."
        action={
          <button
            onClick={onCreateClick}
            style={{
              padding: '0.5rem 1.25rem',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: 'var(--primary)',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Create first snapshot
          </button>
        }
      />
    )
  }

  return (
    <div style={{ overflowX: 'auto' }}>
      <table
        style={{
          width: '100%',
          borderCollapse: 'collapse',
          tableLayout: 'fixed',
        }}
      >
        <thead>
          <tr>
            {HEADERS.map((h, i) => (
              <th
                key={h}
                style={{
                  padding: '0.5rem 0.75rem',
                  fontSize: 11,
                  fontWeight: 600,
                  color: 'var(--fg-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: '0.05em',
                  textAlign: i === HEADERS.length - 1 ? 'right' : 'left',
                  borderBottom: '1px solid var(--border)',
                  width: WIDTHS[i],
                  background: 'var(--surface)',
                }}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {snapshots.map((snap) => (
            <SnapshotRow key={snap.id} snapshot={snap} />
          ))}
        </tbody>
      </table>
    </div>
  )
}
