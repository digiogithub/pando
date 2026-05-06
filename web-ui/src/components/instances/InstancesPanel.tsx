import { useEffect, useRef } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faServer, faSpinner, faSyncAlt } from '@fortawesome/free-solid-svg-icons'
import { useInstancesStore } from '@/stores/instancesStore'
import InstanceCard from './InstanceCard'
import RemoteSessionView from './RemoteSessionView'

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
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--bg)',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '1rem 1.5rem',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
          flexWrap: 'wrap',
          gap: '0.5rem',
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', margin: 0 }}>
          <FontAwesomeIcon icon={faServer} style={{ marginRight: '0.5rem', color: 'var(--primary)' }} />
          Instances{' '}
          <span style={{ fontSize: 13, fontWeight: 400, color: 'var(--fg-muted)' }}>
            ({instances.length})
          </span>
        </h2>
        <button
          onClick={() => void fetchInstances()}
          disabled={loading}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            padding: '0.5rem 0.875rem',
            borderRadius: 'var(--radius-sm)',
            border: '1px solid var(--border)',
            background: 'transparent',
            color: 'var(--fg-muted)',
            fontSize: 13,
            cursor: loading ? 'not-allowed' : 'pointer',
            opacity: loading ? 0.6 : 1,
            fontFamily: 'inherit',
          }}
        >
          {loading ? (
            <FontAwesomeIcon icon={faSpinner} spin style={{ fontSize: 11 }} />
          ) : (
            <FontAwesomeIcon icon={faSyncAlt} style={{ fontSize: 11 }} />
          )}
          Refresh
        </button>
      </div>

      {/* Body: two columns */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Left column: instances list */}
        <div
          style={{
            width: 280,
            flexShrink: 0,
            borderRight: '1px solid var(--border)',
            overflow: 'auto',
          }}
        >
          {loading && instances.length === 0 ? (
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                height: 120,
                color: 'var(--fg-muted)',
                gap: '0.5rem',
                fontSize: 13,
              }}
            >
              <FontAwesomeIcon icon={faSpinner} spin />
              Loading instances…
            </div>
          ) : instances.length === 0 ? (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                padding: '2.5rem 1.5rem',
                color: 'var(--fg-muted)',
                gap: '0.75rem',
                textAlign: 'center',
              }}
            >
              <FontAwesomeIcon icon={faServer} style={{ fontSize: 28, opacity: 0.3 }} />
              <p style={{ margin: 0, fontSize: 13 }}>No running instances found.</p>
              <p style={{ margin: 0, fontSize: 12, color: 'var(--fg-dim)' }}>
                Start another Pando instance to see it listed here.
              </p>
            </div>
          ) : (
            Object.entries(grouped).map(([path, group]) => (
              <div key={path}>
                {/* Group header */}
                <div
                  style={{
                    padding: '0.4rem 1rem',
                    fontSize: 10,
                    fontWeight: 600,
                    color: 'var(--fg-dim)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                    background: 'var(--sidebar-bg)',
                    borderBottom: '1px solid var(--border)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                  title={path}
                >
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
        <div style={{ flex: 1, overflow: 'hidden' }}>
          {!selectedInstance ? (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                height: '100%',
                color: 'var(--fg-muted)',
                gap: '0.75rem',
              }}
            >
              <FontAwesomeIcon icon={faServer} style={{ fontSize: 32, opacity: 0.25 }} />
              <p style={{ margin: 0, fontSize: 14 }}>Select an instance to view its sessions</p>
            </div>
          ) : (
            <RemoteSessionView instance={selectedInstance} />
          )}
        </div>
      </div>
    </div>
  )
}
