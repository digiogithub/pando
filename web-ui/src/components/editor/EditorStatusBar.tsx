import { useEditorStore } from '@pando/client/stores/editorStore'
import { GitBranch } from '@/components/ui/icons'

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
    <div className="editor-status-bar">
      {/* Left: git branch */}
      <div className="editor-status-group">
        <span className="editor-status-branch">
          <GitBranch size={12} />
          {gitBranch}
        </span>
        {activeFile && (
          <>
            <span className="editor-status-sep">|</span>
            <span className="editor-status-path" title={activeFile.path}>
              {activeFile.path}
            </span>
            {activeFile.isDirty && <span className="editor-status-dirty">●</span>}
          </>
        )}
      </div>

      {/* Right: language | encoding | cursor */}
      <div className="editor-status-group">
        {activeFile ? (
          <>
            <span>{displayLanguage}</span>
            <span className="editor-status-sep">|</span>
            <span>UTF-8</span>
            <span className="editor-status-sep">|</span>
            <span>
              Ln {line}, Col {col}
            </span>
          </>
        ) : (
          <span className="editor-status-sep">No file open</span>
        )}
      </div>
    </div>
  )
}
