import { useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlus } from '@fortawesome/free-solid-svg-icons'
import { useSnapshotsStore } from '@/stores/snapshotsStore'
import LoadingSpinner from '@/components/shared/LoadingSpinner'
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
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--bg)',
      }}
    >
      {/* Header bar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '1rem 1.5rem',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
        }}
      >
        <div>
          <h2 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)' }}>
            Snapshots{' '}
            <span style={{ fontSize: 13, fontWeight: 400, color: 'var(--fg-muted)' }}>
              ({snapshots.length} total)
            </span>
          </h2>
        </div>
        <button
          onClick={() => setCreateDialogOpen(true)}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            padding: '0.5rem 1rem',
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
          <FontAwesomeIcon icon={faPlus} style={{ fontSize: 11 }} />
          Create Snapshot
        </button>
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        {loading && snapshots.length === 0 ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
            }}
          >
            <LoadingSpinner size={28} />
          </div>
        ) : (
          <SnapshotTable
            snapshots={snapshots}
            onCreateClick={() => setCreateDialogOpen(true)}
          />
        )}
      </div>

      {/* Create dialog */}
      {createDialogOpen && <CreateSnapshotDialog />}
    </div>
  )
}
