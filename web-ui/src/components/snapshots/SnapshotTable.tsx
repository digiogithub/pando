import { Camera } from '@/components/ui/icons'
import { Button } from '@/components/ui'
import type { Snapshot } from '@pando/client/types'
import EmptyState from '@/components/shared/EmptyState'
import SnapshotRow from './SnapshotRow'

interface SnapshotTableProps {
  snapshots: Snapshot[]
  onCreateClick: () => void
}

export default function SnapshotTable({ snapshots, onCreateClick }: SnapshotTableProps) {
  if (snapshots.length === 0) {
    return (
      <EmptyState
        icon={<Camera size={22} />}
        title="No snapshots yet"
        description="Snapshots let you save and restore session states at any point in time."
        action={
          <Button variant="primary" onClick={onCreateClick}>
            Create first snapshot
          </Button>
        }
      />
    )
  }

  return (
    <div className="view-table-wrap">
      <table className="view-table" style={{ tableLayout: 'fixed' }}>
        <colgroup>
          <col style={{ width: '30%' }} />
          <col style={{ width: '10%' }} />
          <col style={{ width: '12%' }} />
          <col style={{ width: '13%' }} />
          <col style={{ width: '10%' }} />
          <col style={{ width: '15%' }} />
        </colgroup>
        <thead>
          <tr>
            <th>Name</th>
            <th>Session</th>
            <th>Status</th>
            <th>Date</th>
            <th>Size</th>
            <th className="is-numeric">Actions</th>
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
