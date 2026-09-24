import { useEffect, useRef } from 'react'
import { Plus } from '@/components/ui/icons'
import { Button, Spinner } from '@/components/ui'
import { useSnapshotsStore } from '@pando/client/stores/snapshotsStore'
import SnapshotTable from './SnapshotTable'
import CreateSnapshotDialog from './CreateSnapshotDialog'

export default function SnapshotsView() {
  const { snapshots, loading, createDialogOpen, fetchSnapshots, setCreateDialogOpen } =
    useSnapshotsStore()
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    fetchSnapshots()
    intervalRef.current = setInterval(fetchSnapshots, 30_000)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [fetchSnapshots])

  return (
    <div className="view">
      {/* Header bar */}
      <div className="view-header">
        <div className="view-header-text">
          <div className="view-title">
            Snapshots <span className="view-title-count">({snapshots.length} total)</span>
          </div>
        </div>
        <div className="view-header-actions">
          <Button variant="primary" icon={<Plus size={13} />} onClick={() => setCreateDialogOpen(true)}>
            Create Snapshot
          </Button>
        </div>
      </div>

      {/* Content */}
      <div className="view-body">
        {loading && snapshots.length === 0 ? (
          <div className="flex h-full items-center justify-center">
            <Spinner size={26} />
          </div>
        ) : (
          <SnapshotTable snapshots={snapshots} onCreateClick={() => setCreateDialogOpen(true)} />
        )}
      </div>

      {/* Create dialog */}
      {createDialogOpen && <CreateSnapshotDialog />}
    </div>
  )
}
