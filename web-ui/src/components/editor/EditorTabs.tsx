import clsx from 'clsx'
import { useEditorStore } from '@pando/client/stores/editorStore'
import { X } from '@/components/ui/icons'
import { useDialogs } from '@/components/shared/useDialogs'

export default function EditorTabs() {
  const { openFiles, activeFilePath, setActiveFile, closeFile } = useEditorStore()
  const { confirm, dialogs } = useDialogs()

  if (openFiles.length === 0) return null

  const requestClose = async (path: string, isDirty: boolean) => {
    if (isDirty) {
      const confirmed = await confirm({
        title: 'Unsaved changes',
        message: `"${getFileName(path)}" has unsaved changes. Close without saving?`,
        confirmLabel: 'Close without saving',
        dangerous: true,
      })
      if (!confirmed) return
    }
    closeFile(path)
  }

  const handleClose = (e: React.MouseEvent, path: string, isDirty: boolean) => {
    e.stopPropagation()
    void requestClose(path, isDirty)
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
                void requestClose(file.path, file.isDirty)
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
      {dialogs}
    </div>
  )
}
