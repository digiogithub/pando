import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTimes } from '@fortawesome/free-solid-svg-icons'
import { useEditorStore } from '@/stores/editorStore'

export default function EditorTabs() {
  const { openFiles, activeFilePath, setActiveFile, closeFile } = useEditorStore()

  if (openFiles.length === 0) return null

  const handleClose = async (
    e: React.MouseEvent,
    path: string,
    isDirty: boolean,
    _content: string
  ) => {
    e.stopPropagation()
    if (isDirty) {
      const confirmed = window.confirm('File has unsaved changes. Close without saving?')
      if (!confirmed) return
    }
    closeFile(path)
  }

  const getFileName = (path: string) => path.split('/').pop() ?? path

  return (
    <div
      style={{
        height: 36,
        display: 'flex',
        alignItems: 'stretch',
        background: 'var(--sidebar-bg)',
        borderBottom: '1px solid var(--border)',
        overflowX: 'auto',
        overflowY: 'hidden',
        flexShrink: 0,
      }}
    >
      {openFiles.map((file) => {
        const isActive = file.path === activeFilePath
        return (
          <div
            key={file.path}
            onClick={() => setActiveFile(file.path)}
            onMouseDown={(e) => {
              if (e.button === 1) {
                e.preventDefault()
                e.stopPropagation()
                if (file.isDirty) {
                  const confirmed = window.confirm('File has unsaved changes. Close without saving?')
                  if (!confirmed) return
                }
                closeFile(file.path)
              }
            }}
            title={file.path}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              padding: '0 12px 0 14px',
              cursor: 'pointer',
              flexShrink: 0,
              maxWidth: 200,
              minWidth: 80,
              background: isActive ? 'var(--bg)' : 'transparent',
              borderRight: '1px solid var(--border)',
              borderBottom: isActive ? `2px solid var(--primary)` : '2px solid transparent',
              fontSize: 13,
              color: isActive ? 'var(--fg)' : 'var(--fg-muted, #a0a0b0)',
              position: 'relative',
              transition: 'background 0.1s',
            }}
            onMouseEnter={(e) => {
              if (!isActive) {
                ;(e.currentTarget as HTMLDivElement).style.background = 'var(--hover-bg)'
              }
            }}
            onMouseLeave={(e) => {
              if (!isActive) {
                ;(e.currentTarget as HTMLDivElement).style.background = 'transparent'
              }
            }}
          >
            <span
              style={{
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                flex: 1,
              }}
            >
              {getFileName(file.path)}
              {file.isDirty && (
                <span
                  style={{
                    marginLeft: 4,
                    color: 'var(--primary)',
                    fontWeight: 700,
                  }}
                >
                  •
                </span>
              )}
            </span>
            <button
              onClick={(e) => handleClose(e, file.path, file.isDirty, file.content)}
              title="Close tab"
              style={{
                background: 'transparent',
                border: 'none',
                cursor: 'pointer',
                color: 'var(--fg-muted, #a0a0b0)',
                padding: '2px 4px',
                borderRadius: 3,
                display: 'flex',
                alignItems: 'center',
                flexShrink: 0,
                lineHeight: 1,
              }}
              onMouseEnter={(e) => {
                ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)'
                ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--hover-bg)'
              }}
              onMouseLeave={(e) => {
                ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--fg-muted, #a0a0b0)'
                ;(e.currentTarget as HTMLButtonElement).style.background = 'transparent'
              }}
            >
              <FontAwesomeIcon icon={faTimes} style={{ fontSize: 10 }} />
            </button>
          </div>
        )
      })}
    </div>
  )
}
