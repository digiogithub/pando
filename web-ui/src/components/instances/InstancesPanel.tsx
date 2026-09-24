import { useEffect, useRef } from 'react'
import { Server, RefreshCw } from '@/components/ui/icons'
import { Button, Spinner } from '@/components/ui'
import { useInstancesStore } from '@pando/client/stores/instancesStore'
import InstanceCard from './InstanceCard'
import RemoteSessionView from './RemoteSessionView'
import EmptyState from '@/components/shared/EmptyState'

export default function InstancesPanel() {
  const {
    instances,
    selectedInstanceId,
    loading,
    fetchInstances,
    selectInstance,
  } = useInstancesStore()

  const mountedRef = useRef(false)

  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true
      void fetchInstances()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const selectedInstance = instances.find((i) => i.instance_id === selectedInstanceId) ?? null

  // Group instances by path
  const grouped = instances.reduce<Record<string, typeof instances>>((acc, inst) => {
    const key = inst.path
    if (!acc[key]) acc[key] = []
    acc[key].push(inst)
    return acc
  }, {})

  return (
    <div className="view">
      {/* Header */}
      <div className="view-header">
        <div className="view-title">
          <Server size={16} className="text-muted" />
          Instances <span className="view-title-count">({instances.length})</span>
        </div>
        <div className="view-header-actions">
          <Button
            variant="secondary"
            icon={loading ? <Spinner size={12} /> : <RefreshCw size={13} />}
            disabled={loading}
            onClick={() => void fetchInstances()}
          >
            Refresh
          </Button>
        </div>
      </div>

      {/* Body: two columns */}
      <div className="split-pane">
        {/* Left column: instances list */}
        <div className="split-pane-side" style={{ width: 280 }}>
          {loading && instances.length === 0 ? (
            <div className="flex h-32 items-center justify-center gap-2 text-sm text-muted">
              <Spinner size={14} /> Loading instances…
            </div>
          ) : instances.length === 0 ? (
            <EmptyState
              icon={<Server size={22} />}
              title="No running instances found."
              description="Start another Pando instance to see it listed here."
            />
          ) : (
            Object.entries(grouped).map(([path, group]) => (
              <div key={path}>
                {/* Group header */}
                <div className="entity-row-group-header" title={path}>
                  {path.replace(/^\/home\/[^/]+/, '~').replace(/^\/Users\/[^/]+/, '~')}
                </div>
                {group.map((inst) => (
                  <InstanceCard
                    key={inst.instance_id}
                    instance={inst}
                    selected={inst.instance_id === selectedInstanceId}
                    onClick={() => void selectInstance(inst.instance_id)}
                  />
                ))}
              </div>
            ))
          )}
        </div>

        {/* Right column: sessions / stream */}
        <div className="split-pane-main">
          {!selectedInstance ? (
            <div className="centered-fill">
              <Server size={30} className="text-faint opacity-70" />
              <p className="text-sm">Select an instance to view its sessions</p>
            </div>
          ) : (
            <RemoteSessionView instance={selectedInstance} />
          )}
        </div>
      </div>
    </div>
  )
}
