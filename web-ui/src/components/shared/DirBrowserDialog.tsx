import { useCallback, useEffect, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faFolder, faFolderOpen, faArrowUp, faSpinner, faTimes } from '@fortawesome/free-solid-svg-icons'
import api from '@/services/api'

interface BrowseResult {
  path: string
  parent: string
  dirs: string[]
}

export default function DirBrowserDialog({
  initialPath,
  onSelect,
  onClose,
}: {
  initialPath?: string
  onSelect: (path: string) => void
  onClose: () => void
}) {
  const [currentPath, setCurrentPath] = useState(initialPath || '~')
  const [result, setResult] = useState<BrowseResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const browse = useCallback(async (path: string) => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.get<BrowseResult>(`/api/v1/fs/browse?path=${encodeURIComponent(path)}`)
      setResult(data)
      setCurrentPath(data.path)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cannot read directory')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void browse(currentPath)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 2000,
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: 'var(--surface)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius)',
          padding: '1.25rem',
          width: 480,
          maxWidth: '90vw',
          maxHeight: '70vh',
          display: 'flex',
          flexDirection: 'column',
          boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.875rem' }}>
          <span style={{ fontSize: 14, fontWeight: 700, color: 'var(--fg)' }}>Select Directory</span>
          <button
            onClick={onClose}
            style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--fg-muted)', fontSize: 14, padding: '0.25rem' }}
          >
            <FontAwesomeIcon icon={faTimes} />
          </button>
        </div>

        {/* Current path */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
          {result?.parent && (
            <button
              onClick={() => void browse(result.parent)}
              title="Go up"
              style={{
                background: 'none',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                color: 'var(--fg-muted)',
                cursor: 'pointer',
                padding: '0.35rem 0.5rem',
                fontSize: 12,
                flexShrink: 0,
              }}
            >
              <FontAwesomeIcon icon={faArrowUp} />
            </button>
          )}
          <div
            style={{
              flex: 1,
              padding: '0.35rem 0.6rem',
              background: 'var(--input-bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              fontSize: 12,
              color: 'var(--fg)',
              fontFamily: 'monospace',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {result?.path ?? currentPath}
          </div>
        </div>

        {/* Directory list */}
        <div
          style={{
            flex: 1,
            overflowY: 'auto',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            minHeight: 180,
          }}
        >
          {loading ? (
            <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--fg-muted)', fontSize: 13 }}>
              <FontAwesomeIcon icon={faSpinner} spin style={{ marginRight: '0.5rem' }} />
              Loading…
            </div>
          ) : error ? (
            <div style={{ padding: '1rem', color: '#e55', fontSize: 13 }}>{error}</div>
          ) : result && result.dirs.length === 0 ? (
            <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--fg-muted)', fontSize: 13 }}>
              No subdirectories
            </div>
          ) : (
            result?.dirs.map((dir) => {
              const fullDir = `${result.path}/${dir}`
              return (
                <div
                  key={dir}
                  onClick={() => void browse(fullDir)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.5rem',
                    padding: '0.45rem 0.75rem',
                    cursor: 'pointer',
                    fontSize: 13,
                    color: 'var(--fg)',
                    borderBottom: '1px solid var(--border)',
                    transition: 'background 0.1s',
                  }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLDivElement).style.background = 'var(--sidebar-bg)' }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLDivElement).style.background = 'transparent' }}
                >
                  <FontAwesomeIcon icon={faFolder} style={{ color: '#F59E0B', fontSize: 13, flexShrink: 0 }} />
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{dir}</span>
                </div>
              )
            })
          )}
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '0.875rem' }}>
          <button
            onClick={onClose}
            style={{
              padding: '0.4rem 0.875rem',
              background: 'none',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              cursor: 'pointer',
              color: 'var(--fg)',
              fontSize: 13,
              fontFamily: 'inherit',
            }}
          >
            Cancel
          </button>
          <button
            onClick={() => { if (result) { onSelect(result.path); onClose() } }}
            disabled={!result}
            style={{
              padding: '0.4rem 0.875rem',
              background: 'var(--primary)',
              border: 'none',
              borderRadius: 'var(--radius-sm)',
              cursor: result ? 'pointer' : 'not-allowed',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              fontFamily: 'inherit',
              display: 'flex',
              alignItems: 'center',
              gap: '0.375rem',
              opacity: result ? 1 : 0.5,
            }}
          >
            <FontAwesomeIcon icon={faFolderOpen} style={{ fontSize: 11 }} />
            Select
          </button>
        </div>
      </div>
    </div>
  )
}
