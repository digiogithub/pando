import { useState, useMemo } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faFileCode, faChevronDown, faChevronRight } from '@fortawesome/free-solid-svg-icons'
import { useFileChangesStore, type FileChange } from '@/stores/fileChangesStore'
import DiffViewer from './DiffViewer'

export default function FileChangesBar() {
  const changes = useFileChangesStore((s) => s.changes)
  const [collapsed, setCollapsed] = useState(false)
  const [viewingDiff, setViewingDiff] = useState<FileChange | null>(null)

  const fileList = useMemo(() => {
    return Object.values(changes).sort((a, b) => b.lastUpdated - a.lastUpdated)
  }, [changes])

  if (fileList.length === 0) return null

  const totalAdditions = fileList.reduce((sum, f) => sum + f.additions, 0)
  const totalRemovals = fileList.reduce((sum, f) => sum + f.removals, 0)

  return (
    <>
      <div
        style={{
          flexShrink: 0,
          borderTop: '1px solid var(--border)',
          background: 'var(--surface, var(--bg))',
          fontSize: 12,
        }}
      >
        {/* Header */}
        <button
          onClick={() => setCollapsed((v) => !v)}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            width: '100%',
            padding: '6px 12px',
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: 'var(--fg-muted)',
            fontSize: 11,
            fontFamily: "'JetBrains Mono', 'Fira Mono', monospace",
          }}
        >
          <FontAwesomeIcon
            icon={collapsed ? faChevronRight : faChevronDown}
            style={{ fontSize: 9, width: 10 }}
          />
          <FontAwesomeIcon icon={faFileCode} style={{ fontSize: 11, color: 'var(--primary)' }} />
          <span style={{ color: 'var(--fg)' }}>
            {fileList.length} file{fileList.length !== 1 ? 's' : ''} changed
          </span>
          <span style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
            {totalAdditions > 0 && (
              <span style={{ color: '#a6e3a1' }}>+{totalAdditions}</span>
            )}
            {totalRemovals > 0 && (
              <span style={{ color: '#f38ba8' }}>-{totalRemovals}</span>
            )}
          </span>
        </button>

        {/* File list */}
        {!collapsed && (
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 4,
              padding: '0 12px 8px',
              maxHeight: 80,
              overflowY: 'auto',
            }}
          >
            {fileList.map((fc) => (
              <FileChip key={fc.filePath} file={fc} onView={() => setViewingDiff(fc)} />
            ))}
          </div>
        )}
      </div>

      {/* Diff viewer overlay */}
      {viewingDiff && (
        <DiffViewer file={viewingDiff} onClose={() => setViewingDiff(null)} />
      )}
    </>
  )
}

function FileChip({ file, onView }: { file: FileChange; onView: () => void }) {
  return (
    <button
      onClick={onView}
      title={`View diff: ${file.filePath}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '3px 8px',
        borderRadius: 'var(--radius-sm, 4px)',
        border: '1px solid var(--border)',
        background: 'var(--panel, var(--bg))',
        cursor: 'pointer',
        color: 'var(--fg)',
        fontSize: 11,
        fontFamily: "'JetBrains Mono', 'Fira Mono', monospace",
        lineHeight: 1.4,
        transition: 'border-color 0.15s, background 0.15s',
        whiteSpace: 'nowrap',
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.borderColor = 'var(--primary)'
        e.currentTarget.style.background = 'var(--hover-bg, var(--surface))'
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.borderColor = 'var(--border)'
        e.currentTarget.style.background = 'var(--panel, var(--bg))'
      }}
    >
      <span style={{ maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis' }}>
        {file.fileName}
      </span>
      <span style={{ display: 'flex', gap: 4, fontSize: 10 }}>
        {file.additions > 0 && <span style={{ color: '#a6e3a1' }}>+{file.additions}</span>}
        {file.removals > 0 && <span style={{ color: '#f38ba8' }}>-{file.removals}</span>}
      </span>
    </button>
  )
}
