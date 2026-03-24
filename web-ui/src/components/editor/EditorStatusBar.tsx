import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCodeBranch } from '@fortawesome/free-solid-svg-icons'
import { useEditorStore } from '@/stores/editorStore'

interface EditorStatusBarProps {
  gitBranch?: string
}

export default function EditorStatusBar({ gitBranch = 'main' }: EditorStatusBarProps) {
  const { openFiles, activeFilePath } = useEditorStore()

  const activeFile = activeFilePath ? openFiles.find((f) => f.path === activeFilePath) : null

  const language = activeFile?.language ?? '—'
  const line = activeFile?.cursorLine ?? 1
  const col = activeFile?.cursorCol ?? 1

  const displayLanguage = language.charAt(0).toUpperCase() + language.slice(1)

  return (
    <div
      style={{
        height: 28,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '0 12px',
        background: 'var(--sidebar-bg)',
        borderTop: '1px solid var(--border)',
        flexShrink: 0,
        fontSize: 12,
        color: 'var(--fg-muted, #a0a0b0)',
        userSelect: 'none',
      }}
    >
      {/* Left: git branch */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <FontAwesomeIcon icon={faCodeBranch} style={{ fontSize: 11, color: 'var(--primary)' }} />
        <span>{gitBranch}</span>
        {activeFile && (
          <>
            <span style={{ margin: '0 4px', opacity: 0.4 }}>|</span>
            <span
              style={{
                maxWidth: 200,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
              title={activeFile.path}
            >
              {activeFile.path}
            </span>
            {activeFile.isDirty && (
              <span style={{ color: 'var(--warning)', fontWeight: 700 }}>●</span>
            )}
          </>
        )}
      </div>

      {/* Right: language | encoding | cursor */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
        {activeFile && (
          <>
            <span>{displayLanguage}</span>
            <span style={{ opacity: 0.5 }}>|</span>
            <span>UTF-8</span>
            <span style={{ opacity: 0.5 }}>|</span>
            <span>
              Ln {line}, Col {col}
            </span>
          </>
        )}
        {!activeFile && <span style={{ opacity: 0.5 }}>No file open</span>}
      </div>
    </div>
  )
}
