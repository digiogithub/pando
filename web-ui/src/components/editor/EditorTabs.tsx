import clsx from 'clsx'
import { useEditorStore } from '@pando/client/stores/editorStore'
import { X } from '@/components/ui/icons'

export default function EditorTabs() {
  const { openFiles, activeFilePath, setActiveFile, closeFile } = useEditorStore()

  if (openFiles.length === 0) return null

  const handleClose = async (
    e: React.MouseEvent,
    path: string,
    isDirty: boolean,
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
    <div className="editor-tabs" role="tablist" aria-label="Open files">
      {openFiles.map((file) => {
        const isActive = file.path === activeFilePath
        return (
          <button
            key={file.path}
            type="button"
            role="tab"
            aria-selected={isActive}
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
            className={clsx('editor-tab', isActive && 'editor-tab--active')}
          >
            <span className="editor-tab-label">
              {getFileName(file.path)}
              {file.isDirty && <span className="editor-tab-dirty">•</span>}
            </span>
            <span
              role="button"
              aria-label={`Close ${getFileName(file.path)}`}
              onClick={(e) => handleClose(e, file.path, file.isDirty)}
              title="Close tab"
              className="editor-tab-close"
            >
              <X size={12} />
            </span>
          </button>
        )
      })}
    </div>
  )
}
