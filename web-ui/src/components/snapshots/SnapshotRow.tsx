import { useState } from 'react'
import { Undo2, Trash2 } from '@/components/ui/icons'
import { IconButton } from '@/components/ui'
import type { Snapshot } from '@pando/client/types'
import StatusBadge from '@/components/shared/StatusBadge'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useSnapshotsStore } from '@pando/client/stores/snapshotsStore'

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
  const [confirmDelete, setConfirmDelete] = useState(false)
  const { revertSnapshot, deleteSnapshot } = useSnapshotsStore()

  const handleRevert = async () => {
    await revertSnapshot(snapshot.id)
  }

  const handleDelete = async () => {
    await deleteSnapshot(snapshot.id)
    setConfirmDelete(false)
  }

  return (
    <>
      <tr>
        <td className="font-medium" style={{ maxWidth: 160 }}>
          <span className="is-ellipsis block" title={snapshot.name}>
            {snapshot.name}
          </span>
        </td>
        <td className="is-mono">{snapshot.session_id.slice(0, 6)}</td>
        <td>
          <StatusBadge status={snapshot.status} />
        </td>
        <td className="is-muted">{formatDate(snapshot.created_at)}</td>
        <td className="is-muted">{formatSize(snapshot.size)}</td>
        <td className="is-numeric">
          <div className="flex justify-end gap-1.5">
            <IconButton aria-label="Revert to this snapshot" tooltip icon={<Undo2 size={13} />} size="sm" onClick={() => void handleRevert()} />
            <IconButton
              aria-label="Delete snapshot"
              tooltip
              icon={<Trash2 size={13} />}
              size="sm"
              variant="danger"
              onClick={() => setConfirmDelete(true)}
            />
          </div>
        </td>
      </tr>

      {confirmDelete && (
        <ConfirmDialog
          title="Delete Snapshot"
          message={`Are you sure you want to delete "${snapshot.name}"? This action cannot be undone.`}
          confirmLabel="Delete"
          dangerous
          onConfirm={() => void handleDelete()}
          onCancel={() => setConfirmDelete(false)}
        />
      )}
    </>
  )
}
