import { useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faUndo, faTrash } from '@fortawesome/free-solid-svg-icons'
import type { Snapshot } from '@/types'
import StatusBadge from '@/components/shared/StatusBadge'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useSnapshotsStore } from '@/stores/snapshotsStore'

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
  } catch {
    return iso
  }
}

function formatSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${bytes} B`
}

interface SnapshotRowProps {
  snapshot: Snapshot
}

export default function SnapshotRow({ snapshot }: SnapshotRowProps) {
  const [hovered, setHovered] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const { revertSnapshot, deleteSnapshot } = useSnapshotsStore()

  const handleRevert = async () => {
    await revertSnapshot(snapshot.id)
  }

  const handleDelete = async () => {
    await deleteSnapshot(snapshot.id)
    setConfirmDelete(false)
  }

  const cellStyle: React.CSSProperties = {
    padding: '0.625rem 0.75rem',
    fontSize: 13,
    color: 'var(--fg)',
    borderBottom: '1px solid var(--border)',
    verticalAlign: 'middle',
  }

  return (
    <>
      <tr
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          background: hovered ? 'var(--surface)' : 'transparent',
          transition: 'background 0.15s',
        }}
      >
        <td style={{ ...cellStyle, fontWeight: 500, maxWidth: 160 }}>
          <span
            style={{
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              display: 'block',
            }}
            title={snapshot.name}
          >
            {snapshot.name}
          </span>
        </td>
        <td style={{ ...cellStyle, fontFamily: 'monospace', color: 'var(--fg-muted)', fontSize: 12 }}>
          {snapshot.session_id.slice(0, 6)}
        </td>
        <td style={cellStyle}>
          <StatusBadge status={snapshot.status} />
        </td>
        <td style={{ ...cellStyle, color: 'var(--fg-muted)' }}>
          {formatDate(snapshot.created_at)}
        </td>
        <td style={{ ...cellStyle, color: 'var(--fg-muted)' }}>
          {formatSize(snapshot.size)}
        </td>
        <td style={{ ...cellStyle, textAlign: 'right' }}>
          <div style={{ display: 'flex', gap: '0.375rem', justifyContent: 'flex-end' }}>
            <button
              onClick={handleRevert}
              title="Revert to this snapshot"
              style={{
                padding: '0.25rem 0.5rem',
                borderRadius: 'var(--radius-sm)',
                border: '1px solid var(--border)',
                background: 'transparent',
                color: 'var(--fg-muted)',
                cursor: 'pointer',
                fontSize: 12,
                lineHeight: 1,
                fontFamily: 'inherit',
              }}
            >
              <FontAwesomeIcon icon={faUndo} />
            </button>
            <button
              onClick={() => setConfirmDelete(true)}
              title="Delete snapshot"
              style={{
                padding: '0.25rem 0.5rem',
                borderRadius: 'var(--radius-sm)',
                border: '1px solid var(--border)',
                background: 'transparent',
                color: 'var(--error)',
                cursor: 'pointer',
                fontSize: 12,
                lineHeight: 1,
                fontFamily: 'inherit',
              }}
            >
              <FontAwesomeIcon icon={faTrash} />
            </button>
          </div>
        </td>
      </tr>

      {confirmDelete && (
        <ConfirmDialog
          title="Delete Snapshot"
          message={`Are you sure you want to delete "${snapshot.name}"? This action cannot be undone.`}
          confirmLabel="Delete"
          dangerous
          onConfirm={handleDelete}
          onCancel={() => setConfirmDelete(false)}
        />
      )}
    </>
  )
}
